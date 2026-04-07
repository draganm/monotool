# Monotool

A CLI tool for building and deploying containerized applications in a monorepo setup. Monotool automates Docker image building, SHA-based versioning, and deployment through Helm charts and Git operations.

## Getting Started

Initialize a new monotool project:

```bash
monotool init
```

This creates a `.monotool/config.yaml` in your current directory.

## Configuration

All configuration lives in `.monotool/config.yaml`. It defines **images** (how to build containers) and **rollouts** (how to deploy them).

```yaml
images:
  myservice:
    dockerImage: registry.example.com/myservice
    go:
      package: ./cmd/myservice
  mynixservice:
    dockerImage: registry.example.com/mynixservice
    nix:
      file: nix/mynixservice.nix
  mydockerservice:
    dockerImage: registry.example.com/mydockerservice
    docker:
      context: ./services/mydockerservice
      # dockerfile: Dockerfile.prod   # optional, relative to context; defaults to "Dockerfile"

rollouts:
  production:
    gitea:
      repoUrl: https://gitea.example.com/org/deployments.git
    templates: .monotool/templates
    targetPath: manifests
    helmCharts:
      - repository: https://charts.example.com
        chart: myapp
        version: 1.0.0
        releaseName: myservice
        namespace: default
```

## Image Types

Each image must use exactly one build method: `go`, `nix`, or `docker`.

### Go Images

Builds a Docker image from a Go package using `docker buildx`. The image is tagged with a deterministic SHA computed from the Go package source (via [gosha](https://github.com/draganm/gosha)).

```yaml
images:
  api:
    dockerImage: registry.example.com/api
    go:
      package: ./cmd/api
    platform: linux/amd64  # optional, defaults to linux/amd64
```

The Go build uses a multi-stage Dockerfile: it compiles the Go binary with static linking, then copies it into an Alpine-based image.

### Nix Images

Builds a Docker image using a Nix expression (e.g., `pkgs.dockerTools.buildLayeredImage`). The image is tagged with a hash derived from the Nix derivation, evaluated before building. The resulting tarball is loaded into Docker via `docker load`.

```yaml
images:
  worker:
    dockerImage: registry.example.com/worker
    nix:
      file: nix/worker.nix
```

The `file` path is relative to the project root.

#### Example Nix Expression

Here is an example `nix/worker.nix` that builds a Docker image containing a simple Go binary:

```nix
{ pkgs ? import <nixpkgs> {} }:

let
  app = pkgs.buildGoModule {
    pname = "worker";
    version = "0.1.0";
    src = ../.;
    vendorHash = null; # or the appropriate hash
    subPackages = [ "cmd/worker" ];
  };
in
pkgs.dockerTools.buildLayeredImage {
  name = "worker";
  tag = "latest";
  contents = [ app pkgs.cacert ];
  config = {
    Entrypoint = [ "${app}/bin/worker" ];
  };
}
```

The Nix expression must evaluate to a Docker image tarball (the output of `dockerTools.buildLayeredImage` or `dockerTools.buildImage`).

### Docker Images

Builds a Docker image from an arbitrary `Dockerfile` + context directory using `docker buildx`. Use this when your image doesn't fit the Go template or a Nix derivation — e.g., multi-language projects, images based on existing Dockerfiles, or anything with custom build steps.

```yaml
images:
  web:
    dockerImage: registry.example.com/web
    docker:
      context: ./services/web
      # dockerfile: Dockerfile.prod   # optional, relative to the context directory
    platform: linux/amd64             # optional, defaults to linux/amd64
```

- `context` is resolved **relative to the project root** (matching `go.package` and `nix.file`).
- `dockerfile` is resolved **relative to the context directory**, matching Docker's own `docker build -f` convention. If omitted, `Dockerfile` inside the context is used.

The image is tagged with a deterministic hash computed from the Dockerfile contents plus a sorted manifest of the context directory. `.dockerignore` is honored using the same matcher Docker/BuildKit itself uses, so the tag only changes when files Docker would actually send to the daemon change. Negation patterns (`!keep.txt`) work as expected.

## Commands

```bash
# Initialize a new monotool project
monotool init

# List all configured images and their build status
monotool images list

# Build all images that aren't already built
monotool images build

# Deploy images using a rollout configuration
monotool rollout [rollout-name]
```

### Rollout

The `rollout` command builds all images concurrently, pushes them to the registry, then deploys using the configured method (Gitea PR, Helm charts, or both). Image references are passed to rollout templates as `{{ .images.imageName }}`.

## Requirements

- **Go images:** Docker daemon running, `docker` CLI available
- **Nix images:** `nix-build` and `nix-instantiate` available, Docker daemon running
- **Docker images:** Docker daemon running, `docker buildx` available
- **Rollouts:** `git` in PATH; `tea` CLI for Gitea PR creation
