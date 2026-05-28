package github

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/draganm/monotool/rollout/gitops"
)

type GitHubRollout struct {
	RepoURL string `yaml:"repoUrl"`
	Base    string `yaml:"base"`
}

func (g *GitHubRollout) RollOut(ctx context.Context, message string, generate func(dir string) (added, removed []string, err error)) error {
	td, err := os.MkdirTemp("", "")
	if err != nil {
		return fmt.Errorf("could not create a temp dir: %w", err)
	}
	defer os.RemoveAll(td)

	if err := gitops.CloneRepo(ctx, g.RepoURL, td); err != nil {
		return err
	}

	commitTime := time.Now().Format("2006-01-02-15-04-05")
	branchName := fmt.Sprintf("rollout-%s", commitTime)
	if err := gitops.CreateBranch(ctx, td, branchName); err != nil {
		return err
	}

	added, removed, err := generate(td)
	if err != nil {
		return fmt.Errorf("could not generate manifests: %w", err)
	}

	if err := gitops.StageChanges(ctx, td, added, removed); err != nil {
		return fmt.Errorf("could not stage changes: %w", err)
	}

	hasChanges, err := gitops.HasStagedChanges(ctx, td)
	if err != nil {
		return err
	}
	if !hasChanges {
		fmt.Println("no changes to roll out")
		return nil
	}

	commitMessage := fmt.Sprintf("rollout %s\n\n%s", commitTime, message)
	if err := gitops.CreateCommit(ctx, td, commitMessage); err != nil {
		return fmt.Errorf("could not create commit: %w", err)
	}
	if err := gitops.PushToOrigin(ctx, td, branchName); err != nil {
		return fmt.Errorf("could not push: %w", err)
	}

	output, err := createPR(ctx, td, fmt.Sprintf("rollout %s", commitTime), message, g.Base, branchName)
	if err != nil {
		return fmt.Errorf("could not create PR: %w", err)
	}
	fmt.Println(output)
	return nil
}

func createPR(ctx context.Context, dir string, title, body, base, head string) (string, error) {
	args := []string{"pr", "create", "--title", title, "--body", body, "--head", head}
	if base != "" {
		args = append(args, "--base", base)
	}

	cmd := exec.CommandContext(ctx, "gh", args...)
	out := new(bytes.Buffer)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Dir = dir

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("gh pr create failed: %w\n%s", err, out.String())
	}

	return out.String(), nil
}
