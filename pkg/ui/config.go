package ui

import (
	"flag"

	"github.com/grafana/loki/v3/pkg/storage/bucket"
	lokiring "github.com/grafana/loki/v3/pkg/util/ring"
)

type GoldfishConfig struct {
	Enable              bool   `yaml:"enable"`                // Whether to enable the Goldfish query comparison feature.
	CloudSQLUser        string `yaml:"cloudsql_user"`         // CloudSQL username
	CloudSQLHost        string `yaml:"cloudsql_host"`         // CloudSQL host
	CloudSQLPort        int    `yaml:"cloudsql_port"`         // CloudSQL port
	CloudSQLDatabase    string `yaml:"cloudsql_database"`     // CloudSQL database name
	MaxConnections      int    `yaml:"max_connections"`       // Maximum number of database connections
	MaxIdleTime         int    `yaml:"max_idle_time"`         // Maximum idle time for connections in seconds
	GrafanaURL          string `yaml:"grafana_url"`           // Base URL of Grafana instance for explore links
	TracesDatasourceUID string `yaml:"traces_datasource_uid"` // UID of the traces datasource in Grafana
	LogsDatasourceUID   string `yaml:"logs_datasource_uid"`   // UID of the Loki datasource in Grafana
	CellANamespace      string `yaml:"cell_a_namespace"`      // Namespace for Cell A logs
	CellBNamespace      string `yaml:"cell_b_namespace"`      // Namespace for Cell B logs

	// Result storage configuration for fetching raw query results from object storage
	ResultsBackend string        `yaml:"results_backend"` // Results storage backend (gcs, s3)
	ResultsBucket  bucket.Config `yaml:"results_bucket"`  // Bucket configuration for accessing stored results
}

type Config struct {
	Enabled  bool                `yaml:"enabled"` // Whether to enable the UI.
	Debug    bool                `yaml:"debug"`
	Goldfish GoldfishConfig      `yaml:"goldfish"` // Goldfish query comparison configuration
	Ring     lokiring.RingConfig `yaml:"ring"`     // UI ring configuration for cluster member discovery
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, "ui.enabled", false, "Enable the experimental Loki UI.")
	f.BoolVar(&cfg.Debug, "ui.debug", false, "Enable debug logging for the UI.")

	// Ring configuration
	cfg.Ring.RegisterFlagsWithPrefix("ui.", "collectors/", f)

	// Goldfish configuration
	f.BoolVar(&cfg.Goldfish.Enable, "ui.goldfish.enable", false, "Enable the Goldfish query comparison feature.")
	f.StringVar(&cfg.Goldfish.CloudSQLUser, "ui.goldfish.cloudsql-user", "", "CloudSQL username for Goldfish database.")
	f.StringVar(&cfg.Goldfish.CloudSQLHost, "ui.goldfish.cloudsql-host", "127.0.0.1", "CloudSQL host for Goldfish database.")
	f.IntVar(&cfg.Goldfish.CloudSQLPort, "ui.goldfish.cloudsql-port", 3306, "CloudSQL port for Goldfish database.")
	f.StringVar(&cfg.Goldfish.CloudSQLDatabase, "ui.goldfish.cloudsql-database", "goldfish", "CloudSQL database name for Goldfish.")
	f.IntVar(&cfg.Goldfish.MaxConnections, "ui.goldfish.max-connections", 10, "Maximum number of database connections for Goldfish.")
	f.IntVar(&cfg.Goldfish.MaxIdleTime, "ui.goldfish.max-idle-time", 300, "Maximum idle time for database connections in seconds.")
	f.StringVar(&cfg.Goldfish.GrafanaURL, "ui.goldfish.grafana-url", "", "Base URL of Grafana instance for explore links.")
	f.StringVar(&cfg.Goldfish.TracesDatasourceUID, "ui.goldfish.traces-datasource-uid", "", "UID of the traces datasource in Grafana.")
	f.StringVar(&cfg.Goldfish.LogsDatasourceUID, "ui.goldfish.logs-datasource-uid", "", "UID of the Loki datasource in Grafana.")
	f.StringVar(&cfg.Goldfish.CellANamespace, "ui.goldfish.cell-a-namespace", "", "Namespace for Cell A logs.")
	f.StringVar(&cfg.Goldfish.CellBNamespace, "ui.goldfish.cell-b-namespace", "", "Namespace for Cell B logs.")

	// Result storage configuration
	f.StringVar(&cfg.Goldfish.ResultsBackend, "ui.goldfish.results-backend", "", "Results storage backend (gcs, s3) for fetching stored query results.")
	cfg.Goldfish.ResultsBucket.RegisterFlagsWithPrefix("ui.goldfish.results.", f)
}
