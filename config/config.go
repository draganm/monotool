package config

import (
	"github.com/draganm/monotool/image"
	"github.com/draganm/monotool/rollout"
)

// type Deployment struct {
// 	Gitea        *GiteaDeployment `yaml:"gitea"`
// 	Templates    string           `yaml:"templates"`
// 	TargetPath   string           `yaml:"targetPath"`
// 	PruneTargets bool             `yaml:"pruneTargets"`
// }

// type GiteaDeployment struct {
// 	RepoURL string `yaml:"repoUrl"`
// }

type Config struct {
	// ProjectRoot is the location of the parent of the .monotool
	// directory
	ProjectRoot string                      `yaml:"-"`
	Images      map[string]*image.Image     `yaml:"images"`
	RollOuts    map[string]*rollout.Rollout `yaml:"rollouts"`
}
