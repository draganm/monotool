package gitea

import "context"

func (g *GiteaRollout) RollOut(ctx context.Context, generate func(dir string) error) error {
	return nil
}
