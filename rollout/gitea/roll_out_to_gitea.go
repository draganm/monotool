package gitea

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/draganm/monotool/rollout/gitops"
)

type GiteaRollout struct {
	RepoURL string `yaml:"repoUrl"`
}

func (g *GiteaRollout) RollOut(ctx context.Context, message string, generate func(dir string) (added, removed []string, err error)) error {
	td, err := os.MkdirTemp("", "")
	if err != nil {
		return fmt.Errorf("could not create a temp dir: %w", err)
	}

	defer func() {
		os.RemoveAll(td)
	}()

	err = gitops.CloneRepo(ctx, g.RepoURL, td)
	if err != nil {
		return err
	}

	commitTime := time.Now().Format("2006-01-02-15-04-05")

	branchName := fmt.Sprintf("rollout-%s", commitTime)

	err = gitops.CreateBranch(ctx, td, branchName)
	if err != nil {
		return err
	}

	_, _, err = generate(td)
	if err != nil {
		return fmt.Errorf("could not generate manifests: %w", err)
	}

	err = gitops.AddFiles(ctx, td)
	if err != nil {
		return fmt.Errorf("could not add generated files: %w", err)
	}

	commitMessage := fmt.Sprintf("rollout %s\n\n%s", commitTime, message)

	err = gitops.CreateCommit(ctx, td, commitMessage)
	if err != nil {
		return fmt.Errorf("could not create commit: %w", err)
	}

	err = gitops.PushToOrigin(ctx, td, branchName)
	if err != nil {
		return fmt.Errorf("could not push: %w", err)
	}

	output, err := createPR(ctx, td, fmt.Sprintf("rollout %s", commitTime), message)
	if err != nil {
		return fmt.Errorf("could not create PR: %w", err)
	}

	fmt.Println(output)

	return nil
}

func createPR(ctx context.Context, dir string, title, description string) (string, error) {
	cmd := exec.CommandContext(ctx, "tea", "pr", "create", "--title", title, "--description", description)
	out := new(bytes.Buffer)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Dir = dir

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("tea pr create failed: %w\n%s", err, out.String())
	}

	return out.String(), nil
}
