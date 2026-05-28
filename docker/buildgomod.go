package docker

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"text/template"

	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/packages"
)

//go:embed go-dockerfile
var dockerfileTemplate string

type DockerfileData struct {
	PackagePath string
	GoVersion   string
}

func BuildGoMod(ctx context.Context, mainPackagePath, imageName, platform string, out io.Writer) error {
	pkg, err := packages.Load(&packages.Config{
		Mode:    packages.NeedModule | packages.NeedName,
		Context: ctx,
		Dir:     mainPackagePath,
	}, ".")
	if err != nil {
		return fmt.Errorf("could not get main package: %w", err)
	}

	mod := pkg[0].Module
	if mod.Error != nil {
		return fmt.Errorf("could not get module info for the main package: %w", err)
	}

	modData, err := os.ReadFile(mod.GoMod)
	if err != nil {
		return fmt.Errorf("could not read %s: %w", mod.GoMod, err)
	}

	modFile, err := modfile.Parse(mod.GoMod, modData, nil)
	if err != nil {
		return fmt.Errorf("could not parse go.mod file: %w", err)
	}

	fullPath := pkg[0].PkgPath
	path := modFile.Module.Mod.Path
	shortPath := strings.TrimPrefix(fullPath, path)
	shortPath = strings.TrimPrefix(shortPath, "/")

	templ, err := template.New("dockerfile").Parse(dockerfileTemplate)
	if err != nil {
		return fmt.Errorf("could not parse dockerfile template: %w", err)
	}
	rendered := &bytes.Buffer{}
	err = templ.Execute(rendered, DockerfileData{
		PackagePath: shortPath,
		GoVersion:   modFile.Go.Version,
	})
	if err != nil {
		return fmt.Errorf("could not render dockerfile template: %w", err)
	}
	dockerRoot := mod.Dir

	genCmd := exec.CommandContext(ctx, "go", "generate", "./...")
	genCmd.Dir = dockerRoot
	genCmd.Stdout = out
	genCmd.Stderr = out
	if err := genCmd.Run(); err != nil {
		return fmt.Errorf("go generate ./... failed: %w", err)
	}

	tempDockerfile, err := os.CreateTemp("", "")
	if err != nil {
		return fmt.Errorf("could not create temp dockerfile: %w", err)
	}
	defer os.Remove(tempDockerfile.Name())
	defer tempDockerfile.Close()

	_, err = tempDockerfile.Write(rendered.Bytes())
	if err != nil {
		return fmt.Errorf("could not write to temp docker file: %w", err)
	}

	if err := tempDockerfile.Close(); err != nil {
		return fmt.Errorf("could not close temp docker file: %w", err)
	}

	cmd := exec.CommandContext(ctx, "docker", "buildx", "build", "--platform", platform, "-t", imageName, "-f", tempDockerfile.Name(), "--progress", "plain", dockerRoot)
	cmd.Stdout = out
	cmd.Stderr = out

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}
	return nil
}
