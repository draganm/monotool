# Nix Image Building for Monotool

## Context

Monotool currently builds Docker images exclusively from Go packages using a Dockerfile template and `docker buildx`. Users who use Nix want to build Docker images via Nix's `dockerTools` (e.g., `buildLayeredImage`) and push them to registries without going through the local Docker daemon. This feature adds a `nix:` image type alongside the existing `go:` image type.

## Design Decisions

- **Config format:** `.nix` file path (relative to project root), not flake attributes
- **Versioning:** Nix derivation hash from `nix-instantiate`, evaluated before building
- **Registry interaction:** `skopeo copy` to push directly to registry (no `docker load`)
- **Exclusivity:** `go:` and `nix:` are mutually exclusive per image definition
- **Architecture:** Extend existing `Image` struct with a `Nix` field (no interface refactor)

## Configuration

```yaml
images:
  myservice:
    dockerImage: registry.example.com/myservice
    nix:
      file: nix/myservice-image.nix
  goservice:
    dockerImage: registry.example.com/goservice
    go:
      package: ./cmd/goservice
```

The `nix.file` path is relative to the project root. The `.nix` expression must evaluate to a Docker image (e.g., using `pkgs.dockerTools.buildLayeredImage`).

Validation: config loading rejects images with both `go:` and `nix:` set, or neither set.

## Struct Changes

### `image/image.go`

```go
type NixImage struct {
    File string `yaml:"file"`
}

type Image struct {
    Go          *GoImage  `yaml:"go"`
    Nix         *NixImage `yaml:"nix"`
    DockerImage string    `yaml:"dockerImage"`
    Platform    string    `yaml:"platform"`
}
```

## New Package: `nix/`

A new `nix` package parallel to `docker/` with these functions:

### `nix/instantiate.go`
- `Instantiate(ctx context.Context, nixFile string) (drvPath string, err error)` — runs `nix-instantiate <nixFile>`, returns the `.drv` store path
- `DrvHash(drvPath string) string` — extracts the hash from the derivation store path (first 32 chars after `/nix/store/`), truncates to 8 chars for the image tag

### `nix/build.go`
- `Build(ctx context.Context, nixFile string) (resultPath string, err error)` — runs `nix-build <nixFile>`, returns the `./result` symlink target (the tarball path in the Nix store)

### `nix/skopeo.go`
- `SkopeoImageExists(ctx context.Context, imageRef string) (bool, error)` — runs `skopeo inspect docker://<imageRef>`, returns true if found
- `SkopeoCopy(ctx context.Context, archivePath string, imageRef string) error` — runs `skopeo copy docker-archive:<archivePath> docker://<imageRef>`

## Image Method Changes

### `calculateHash(projectRoot string) ([]byte, error)`
- **Go path (unchanged):** uses gosha
- **Nix path:** runs `nix-instantiate` on the nix file, extracts derivation hash, returns it as bytes

### `DockerImageName(projectRoot string) (string, error)`
- **Go path (unchanged):** `dockerImage:gosha-hash[:8]`
- **Nix path:** `dockerImage:drv-hash[:8]`

### `IsAlreadyBuilt(ctx context.Context, projectRoot string) (bool, error)`
- **Go path (unchanged):** checks docker locally + registry via manifest inspect
- **Nix path:** uses `skopeo inspect` to check registry only (no local docker daemon involved)

### `Build(ctx context.Context, projectRoot string) error`
- **Go path (unchanged):** docker buildx
- **Nix path:** runs `nix-build`, then `skopeo copy` to push to registry

## Build Flow for Nix Images

```
1. nix-instantiate nix/myservice.nix
   -> /nix/store/abc123...xyz-docker-image.drv
   -> tag = abc12345 (first 8 chars of hash)

2. skopeo inspect docker://registry.example.com/myservice:abc12345
   -> If found: skip (already built and pushed)

3. nix-build nix/myservice.nix
   -> /nix/store/def456...-docker-image.tar.gz

4. skopeo copy docker-archive:/nix/store/def456...-docker-image.tar.gz \
               docker://registry.example.com/myservice:abc12345
```

## Integration with Rollouts

No changes needed. Rollouts call `image.DockerImageName()` to get the full image reference and use it in templates. The nix path returns the same `name:tag` format.

The rollout command's concurrent build logic (semaphores, goroutines) works unchanged — nix images participate in the same pool.

One difference: for nix images, `Build()` already pushes via skopeo, so the rollout command's separate `docker.Push()` call should be skipped for nix images. This requires a small check: add an `IsNix() bool` method or check `i.Nix != nil` in the rollout command.

## Files to Modify

1. **`image/image.go`** — Add `NixImage` struct, `Nix` field, branch logic in all methods
2. **`nix/instantiate.go`** (new) — `Instantiate()`, `DrvHash()`
3. **`nix/build.go`** (new) — `Build()`
4. **`nix/skopeo.go`** (new) — `SkopeoImageExists()`, `SkopeoCopy()`
5. **`config/load.go`** — Add validation (exactly one of `go:` / `nix:` per image)
6. **`command/rollout/command.go`** — Skip `docker.Push()` for nix images (skopeo handles it in Build)
7. **`command/images/build/command.go`** — May need minor adjustment for nix images (no local docker check)

## Verification

1. Create a test `.nix` file that uses `dockerTools.buildLayeredImage` to produce a simple image
2. Add a nix image entry to `.monotool/config.yaml`
3. Run `monotool images list` — should show the nix image with correct tag
4. Run `monotool images build` — should run nix-build and show success
5. Run `go test ./...` — all existing tests still pass
6. Verify `nix-instantiate` + `nix-build` + `skopeo copy` are called correctly (can test with a local registry)
