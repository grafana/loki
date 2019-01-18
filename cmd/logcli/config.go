package main

// Config is the root config for Logcli.
type Config struct {
	Addr     string `yaml:"addr,omitempty"`
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
}

func getConfig(configFile string) (*Config, error) {
	var config Config

	// if not specify config file, keep same with default value
	if configFile == "" {
		config = Config{Addr: "https://logs-us-west1.grafana.net"}
		return &config, nil
	}

	return &config, nil
}
