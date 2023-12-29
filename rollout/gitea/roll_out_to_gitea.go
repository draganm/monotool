package gitea

import (
	"context"
	"fmt"
	"os"
	"time"
)

type GiteaRollout struct {
	RepoURL string `yaml:"repoUrl"`
}

func (g *GiteaRollout) RollOut(ctx context.Context, generate func(dir string) error) error {
	td, err := os.MkdirTemp("", "")
	if err != nil {
		return fmt.Errorf("could not create a temp dir: %w", err)
	}

	defer func() {
		os.RemoveAll(td)
	}()

	err = cloneRepo(ctx, g.RepoURL, td)
	if err != nil {
		return err
	}

	commitTime := time.Now().Format("2006-01-02-15-04-05")

	branchName := fmt.Sprintf("rollout-%s", commitTime)

	err = createBranch(ctx, td, branchName)
	if err != nil {
		return err
	}

	err = generate(td)
	if err != nil {
		return fmt.Errorf("could not generate manifests: %w", err)
	}

	err = addFilesToGit(ctx, td)
	if err != nil {
		return fmt.Errorf("could not add generated files: %w", err)
	}

	err = createCommit(ctx, td, fmt.Sprintf("rollout %s", commitTime))
	if err != nil {
		return fmt.Errorf("could not create commit: %w", err)
	}

	err = pushToOrigin(ctx, td, branchName)
	if err != nil {
		return fmt.Errorf("could not push: %w", err)
	}

	output, err := createPR(ctx, td, fmt.Sprintf("rollout %s", commitTime), "")
	if err != nil {
		return fmt.Errorf("could not create PR: %w", err)
	}

	fmt.Println(output)

	return nil
}
