package config

type Binary struct {
	// ModulePath is path to the go module relative to the
	// position of parent of the .monotool directory
	ModulePath string `yaml:"modulePath"`
	ImageName  string `yaml:"imageName"`
}

type Config struct {
	// ProjectRoot is the location of the parent of the .monotool
	// directory
	ProjectRoot string             `yaml:"-"`
	Binaries    map[string]*Binary `yaml:"binaries"`
}
