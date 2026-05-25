package gitops

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

func CloneRepo(ctx context.Context, url string, dir string) error {
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

func CreateBranch(ctx context.Context, dir string, branchName string) error {
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

// StageChanges runs `git add` for every added path and `git rm` for every
// removed path. Paths must be relative to dir or absolute paths inside it. No
// other paths are staged.
func StageChanges(ctx context.Context, dir string, added, removed []string) error {
	if len(added) > 0 {
		args := append([]string{"add", "--"}, added...)
		cmd := exec.CommandContext(ctx, "git", args...)
		out := new(bytes.Buffer)
		cmd.Stdout = out
		cmd.Stderr = out
		cmd.Dir = dir
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("git add failed: %w\n%s", err, out.String())
		}
	}
	if len(removed) > 0 {
		args := append([]string{"rm", "--quiet", "--"}, removed...)
		cmd := exec.CommandContext(ctx, "git", args...)
		out := new(bytes.Buffer)
		cmd.Stdout = out
		cmd.Stderr = out
		cmd.Dir = dir
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("git rm failed: %w\n%s", err, out.String())
		}
	}
	return nil
}

// HasStagedChanges reports whether any changes are staged in dir.
func HasStagedChanges(ctx context.Context, dir string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "diff", "--cached", "--name-only")
	out := new(bytes.Buffer)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("git diff --cached failed: %w\n%s", err, out.String())
	}
	return out.Len() > 0, nil
}

func CreateCommit(ctx context.Context, dir string, message string) error {
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

func PushToOrigin(ctx context.Context, dir string, branchName string) error {
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
