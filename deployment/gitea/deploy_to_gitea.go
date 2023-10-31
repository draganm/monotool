package gitea

import "context"

func (g *GiteaDeployment) Deploy(ctx context.Context, generate func(dir string) error) error {
	return nil
}
