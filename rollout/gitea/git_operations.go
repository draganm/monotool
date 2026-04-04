package gitea

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

func cloneRepo(ctx context.Context, url string, dir string) error {
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "--quiet", url, dir)
	out := new(bytes.Buffer)
	cmd.Stdout = out
	cmd.Stderr = out

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("git clone failed: %w\n%s", err, out.String())
	}

	return nil
}

func createBranch(ctx context.Context, dir string, branchName string) error {
	cmd := exec.CommandContext(ctx, "git", "checkout", "-q", "-b", branchName)
	out := new(bytes.Buffer)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Dir = dir

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("git create branch failed: %w\n%s", err, out.String())
	}

	return nil
}

func addFilesToGit(ctx context.Context, dir string) error {
	cmd := exec.CommandContext(ctx, "git", "add", ".")
	out := new(bytes.Buffer)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Dir = dir

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("git add failed: %w\n%s", err, out.String())
	}

	return nil
}

func createCommit(ctx context.Context, dir string, message string) error {
	cmd := exec.CommandContext(ctx, "git", "commit", "-m", message)
	out := new(bytes.Buffer)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Dir = dir

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("git commit failed: %w\n%s", err, out.String())
	}

	return nil
}

func pushToOrigin(ctx context.Context, dir string, branchName string) error {
	cmd := exec.CommandContext(ctx, "git", "push", "origin", branchName)
	out := new(bytes.Buffer)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Dir = dir

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("git push failed: %w\n%s", err, out.String())
	}

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
