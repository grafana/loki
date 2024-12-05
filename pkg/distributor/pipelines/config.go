package pipelines

type Action string

type Stage struct {
	Action Action            `yaml:"action"`
	Config map[string]string `yaml:"config"`
}

type Pipeline struct {
	Match  string  `yaml:"match"`
	Stages []Stage `yaml:"stages"`
}

type IngestPipelineConfig struct {
	Enabled   bool       `yaml:"enabled"`
	Pipelines []Pipeline `yaml:"pipelines"`
}

type RuntimeOverrides interface {
	GetIngestPipelines(tenant string) IngestPipelineConfig
}
