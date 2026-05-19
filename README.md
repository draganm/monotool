# Monotool

A small CLI for building and shipping container images out of a monorepo via GitOps.

## What it does

In a monorepo you typically have many services that share code. Monotool lets you describe each service once, gives every container image a deterministic tag derived from its source, and ships the whole set to a cluster by opening a PR against your GitOps manifests repository.

Concretely, monotool takes care of three things:

1. **Building images.** Go modules or arbitrary `Dockerfile`s are all first-class. Builds run concurrently.
2. **Tagging them deterministically.** The tag for an image is a hash of its source — same source, same tag, every time. If the tag already exists in the registry, the build is skipped.
3. **Rolling them out.** It clones a manifests repository, renders templates with the freshly-computed image references, opens a PR, and prints the URL.

## Philosophy

- **Source content is the version.** Tags come from hashes of the inputs (Go package source, Dockerfile + build context). This makes rebuilds idempotent and rollouts diff-friendly — if nothing changed, the manifests are identical.
- **Do as little work as possible.** Before building, monotool checks whether the image already exists locally or in the registry. If yes, it's reused; if no, it's built, pushed, and tagged.
- **GitOps, not `kubectl apply`.** Rollouts produce a commit and a pull request against a manifests repository. Promotion, review, and rollback are then just git operations.
- **One config file.** Everything lives in `.monotool/config.yaml`. The file is discovered by walking up from the current directory, so the tool works from any subdirectory of the monorepo.

## Getting started

```bash
monotool init
```

This creates `.monotool/config.yaml` in the current directory. Edit it to describe your images and rollouts, then:

```bash
monotool rollout -m "ship the new login flow"
```

## The configuration file

`.monotool/config.yaml` has two top-level keys: `images` and `rollouts`.

```yaml
images:
  api:
    dockerImage: registry.example.com/api
    go:
      package: ./cmd/api

  web:
    dockerImage: registry.example.com/web
    docker:
      context: ./services/web

rollouts:
  production:
    gitea:
      repoUrl: https://gitea.example.com/org/deployments.git
    templates: .monotool/templates
    targetPath: manifests/production
    pruneTargets: true
```

## Images

Every image entry has a `dockerImage` (the registry path **without** a tag) and exactly one of `go` or `docker`. The tag is computed automatically.

```yaml
images:
  myservice:
    dockerImage: registry.example.com/myservice
    platform: linux/amd64        # optional, defaults to linux/amd64
    go: { package: ./cmd/myservice }
```

### Go images

Compiles a Go binary and packages it in a minimal Alpine image. The tag is the first 8 bytes of [gosha](https://github.com/draganm/gosha)'s content hash over the Go package and its imports.

```yaml
images:
  api:
    dockerImage: registry.example.com/api
    go:
      package: ./cmd/api
```

### Docker images

Builds an arbitrary `Dockerfile` against a build context using `docker buildx`. Use this when the Go template doesn't fit — multi-language services, legacy projects, anything with custom build steps.

```yaml
images:
  web:
    dockerImage: registry.example.com/web
    docker:
      context: ./services/web
      # dockerfile: Dockerfile.prod   # optional, relative to context; default "Dockerfile"
    platform: linux/amd64
```

- `context` is resolved **relative to the project root** (the directory that contains `.monotool/`).
- `dockerfile` is resolved **relative to the context directory**, mirroring `docker build -f`. Defaults to `Dockerfile`.

The tag is a hash of the Dockerfile contents plus a sorted manifest of the context. `.dockerignore` is honored using the same matcher BuildKit uses, including negation patterns (`!keep.txt`). The tag only changes when files Docker would actually ship to the daemon change.

## Rollouts

A rollout is a named recipe for building all configured images and producing a PR against a manifests repository. Each rollout points at one GitOps repository hosted on **either Gitea or GitHub** (exactly one is required) and a directory of templates.

```yaml
rollouts:
  staging:
    github:                                    # OR `gitea:`
      repoUrl: https://github.com/org/deployments.git
      # base: main                             # optional target branch for the PR
    templates: .monotool/templates             # template source directory
    targetPath: manifests/staging              # where to write rendered manifests in the repo
    pruneTargets: true                         # remove existing manifests under targetPath before writing
```

### How `monotool rollout` works

```bash
monotool rollout [rollout-name] -m "rollout message"
```

1. Compute the deterministic tag for every image listed in `images`.
2. For each image: skip if the registry already has it; otherwise build (locally or pull) and push.
3. Once all images are available in the registry, clone the rollout's GitOps repo into a temp directory.
4. Create a fresh branch (`rollout-YYYY-MM-DD-hh-mm-ss`).
5. Render every `.yaml`/`.yml` file under `templates` into `targetPath`, interpolating values (see below).
6. If `pruneTargets` is true, existing top-level directories under `targetPath` are removed before rendering, so the manifests are an exact mirror of the templates.
7. Commit, push, and open a PR with the rollout message as the body. The PR URL is printed.

The `-m` / `--message` flag is required and ends up in both the commit message and the PR description.

If only one rollout is configured the name argument can be omitted.

### Templates and interpolation

Templates are plain YAML files under the `templates` directory. They are rendered with [manifestor/interpolate](https://github.com/draganm/manifestor), which performs `{{ ... }}` substitution against a values map. Monotool passes one value: `images`, a map from image name to fully-tagged image reference.

```yaml
# .monotool/templates/api/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
spec:
  template:
    spec:
      containers:
        - name: api
          image: "{{ .images.api }}"
```

After a rollout, this becomes:

```yaml
image: registry.example.com/api:9f3a1c4b2e5d6a78
```

The directory structure under `templates` is preserved under `targetPath`.

### Provider: Gitea or GitHub

The two providers do the same thing — clone, branch, commit, push, open a PR — and differ only in which CLI they shell out to for the PR step:

| Provider | Block in config | PR creation |
|----------|-----------------|-------------|
| Gitea    | `gitea:`        | `tea pr create` |
| GitHub   | `github:`       | `gh pr create` |

Both expect `repoUrl` to be a URL you have credentials for (SSH or HTTPS). The GitHub block also accepts an optional `base` field to target a non-default branch.

A rollout must configure exactly one of the two — having both, or neither, is an error.

## Commands

```bash
monotool init                       # write .monotool/config.yaml in the current directory
monotool rollout [name] -m "..."    # build, push, and open a PR for the named rollout (name optional if only one exists)
```

## Requirements

- **Go images:** Docker daemon running, `docker` CLI with `buildx` available.
- **Docker images:** Docker daemon running, `docker buildx` available.
- **Rollouts:** `git` in `PATH`. `tea` for Gitea PRs, `gh` for GitHub PRs — only the one matching your configured provider.
