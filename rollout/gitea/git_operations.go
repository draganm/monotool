package gitea

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func cloneRepo(ctx context.Context, url string, dir string) error {
	cmd := exec.Command("git", "clone", "--depth", "1", "--quiet", url, dir)
	out := new(bytes.Buffer)
	cmd.Stdout = out
	cmd.Stderr = out

	err := cmd.Run()
	if err != nil {
		b := new(strings.Builder)
		b.WriteString("git clone failed: %w\n")
		b.Write(out.Bytes())
		return fmt.Errorf(b.String(), err)
	}

	return nil
}

func createBranch(ctx context.Context, dir string, branchName string) error {
	cmd := exec.Command("git", "checkout", "-q", "-b", branchName)
	out := new(bytes.Buffer)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Dir = dir

	err := cmd.Run()
	if err != nil {
		b := new(strings.Builder)
		b.WriteString("git create branch failed: %w\n")
		b.Write(out.Bytes())
		return fmt.Errorf(b.String(), err)
	}

	return nil
}

func addFilesToGit(ctx context.Context, dir string) error {
	cmd := exec.Command("git", "add", ".")
	out := new(bytes.Buffer)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Dir = dir

	err := cmd.Run()
	if err != nil {
		b := new(strings.Builder)
		b.WriteString("git add failed: %w\n")
		b.Write(out.Bytes())
		return fmt.Errorf(b.String(), err)
	}

	return nil
}

func createCommit(ctx context.Context, dir string, message string) error {
	cmd := exec.Command("git", "commit", "-m", message)
	out := new(bytes.Buffer)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Dir = dir

	err := cmd.Run()
	if err != nil {
		b := new(strings.Builder)
		b.WriteString("git commit failed: %w\n")
		b.Write(out.Bytes())
		return fmt.Errorf(b.String(), err)
	}

	return nil
}

func pushToOrigin(ctx context.Context, dir string, branchName string) error {
	cmd := exec.Command("git", "push", "origin", branchName)
	out := new(bytes.Buffer)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Dir = dir

	err := cmd.Run()
	if err != nil {
		b := new(strings.Builder)
		b.WriteString("git push failed: %w\n")
		b.Write(out.Bytes())
		return fmt.Errorf(b.String(), err)
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
		b := new(strings.Builder)
		b.WriteString("tea pr create: %w\n")
		b.Write(out.Bytes())
		return "", fmt.Errorf(b.String(), err)
	}

	return out.String(), nil
}
