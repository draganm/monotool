package config

type Container struct {
	Go    *GoContainer `yaml:"go"`
	Image string       `yaml:"image"`
}

type GoContainer struct {
	ModulePath string `yaml:"modulePath"`
}

type Config struct {
	// ProjectRoot is the location of the parent of the .monotool
	// directory
	ProjectRoot string                `yaml:"-"`
	Containers  map[string]*Container `yaml:"containers"`
}
