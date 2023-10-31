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
	"time"

	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/packages"
)

//go:embed go-dockerfile
var dockerfileTemplate string

type DockerfileData struct {
	PackagePath string
	GoVersion   string
}

func BuildGoMod(ctx context.Context, mainPackagePath string, imageName string) error {
	startTime := time.Now()
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

	templ := template.New("dockerfile")
	templ.Parse(dockerfileTemplate)
	rendered := &bytes.Buffer{}
	err = templ.Execute(rendered, DockerfileData{
		PackagePath: shortPath,
		GoVersion:   modFile.Go.Version,
	})
	if err != nil {
		return fmt.Errorf("could not render dockerfile template: %w", err)
	}
	dockerRoot := mod.Dir

	tempDockerfile, err := os.CreateTemp("", "")
	if err != nil {
		return fmt.Errorf("could not create temp dockerfile: %w", err)
	}

	defer tempDockerfile.Close()

	_, err = tempDockerfile.Write(rendered.Bytes())
	if err != nil {
		return fmt.Errorf("could not write to temp docker file: %w", err)
	}

	err = tempDockerfile.Close()
	if err != nil {
		return fmt.Errorf("could not close temp docker file: %w", err)
	}

	cmd := exec.CommandContext(ctx, "docker", "buildx", "build", "-t", imageName, "-f", tempDockerfile.Name(), "--progress", "plain", dockerRoot)
	out := new(bytes.Buffer)

	dp := dotPrinter()

	fmt.Print("\tbuilding image: ")

	cmd.Stdout = io.MultiWriter(dp, out)
	cmd.Stderr = io.MultiWriter(dp, out)
	err = cmd.Run()
	dp.Close()
	fmt.Println()
	if err != nil {
		return fmt.Errorf("docker build failed (%w):\n%s", err, out.String())
	}
	fmt.Println("\t✅ build successful, build time:", time.Since(startTime))

	return nil

}

func dotPrinter() io.WriteCloser {
	r, w := io.Pipe()
	go func() {
		buffer := make([]byte, 128)
		for {
			n, err := r.Read(buffer)
			if n > 0 {
				cnt := strings.Count(string(buffer), "\n")
				for i := 0; i < cnt; i++ {
					fmt.Print(".")
				}
			}
			if err != nil {
				return
			}
		}
	}()

	return w
}
