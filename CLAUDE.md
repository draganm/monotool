# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

Monotool is a Go-based CLI tool for building and deploying containerized applications in a monorepo setup. It automates Docker image building, versioning (using SHA-based tags), and deployment through Git-based GitOps workflows.

## Commands

### Building and Running

```bash
# Build the CLI tool
go build -o monotool

# Run tests (currently no tests exist)
go test ./...

# Ensure dependencies are up to date
go mod tidy

# Build with specific output
go build -o bin/monotool main.go
```

### CLI Usage

```bash
# Initialize a new monotool configuration
./monotool init

# Deploy/rollout images
./monotool rollout [rollout-name]
```

## Architecture

### Core Components

1. **Configuration System** (`config/`)
   - Loads configuration from `.monotool/config.yaml`
   - Defines images (Docker configurations) and rollouts (deployment targets)
   - Config structure includes project root path, image definitions with Go package paths, and rollout definitions

2. **Image Management** (`image/`, `docker/`)
   - Calculates SHA-based versioning using `gosha` library for Go packages
   - Builds Docker images from Go modules using embedded Dockerfile template
   - Handles Docker operations: build, pull, push, and existence checks
   - Default platform: `linux/amd64`

3. **Rollout System** (`rollout/`)
   - Supports multiple Git providers (Gitea, GitHub) for GitOps rollouts
   - Concurrent image building with semaphore-based rate limiting
   - Progress tracking with UI progress bars
   - Template-based deployment configuration

### Key Implementation Details

- **SHA-based Versioning**: Images are tagged with first 8 bytes of package SHA for deterministic versioning
- **Concurrent Operations**: Uses goroutines with semaphores (4 for builds, 10 for image checks)
- **Docker Context**: Builds use custom context with Go module files
- **Template System**: Rollouts use Go templates with image references passed as values

### Configuration Structure

The `.monotool/config.yaml` file defines:
- `images`: Map of image names to Docker/Go configurations
- `rollouts`: Map of rollout names to deployment configurations (Gitea or GitHub repos)

Each image configuration includes:
- `dockerImage`: Base Docker image name (without tag)
- `go.package`: Path to Go main package relative to project root

## Development Notes

- The tool expects a `.monotool` directory in the project root
- Docker operations require Docker daemon to be running
- Gitea rollouts require Git to be available in PATH
- The tool uses the urfave/cli/v2 framework for CLI structure