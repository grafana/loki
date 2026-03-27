package goldfish

// StorageConfig defines storage backend configuration
type StorageConfig struct {
	Type string `yaml:"type"` // "cloudsql", "rds", "mysql", or empty string for no storage

	// Direct MySQL connection
	MySQLHost     string `yaml:"mysql_host"`
	MySQLPort     int    `yaml:"mysql_port"`
	MySQLDatabase string `yaml:"mysql_database"`
	MySQLUser     string `yaml:"mysql_user"`
	// MySQLPassword provided via GOLDFISH_DB_PASSWORD environment variable

	// CloudSQL specific (via proxy)
	CloudSQLHost     string `yaml:"cloudsql_host"`
	CloudSQLPort     int    `yaml:"cloudsql_port"`
	CloudSQLDatabase string `yaml:"cloudsql_database"`
	CloudSQLUser     string `yaml:"cloudsql_user"`
	// CloudSQLPassword provided via GOLDFISH_DB_PASSWORD environment variable

	// RDS specific
	RDSEndpoint string `yaml:"rds_endpoint"` // e.g., "mydb.123456789012.us-east-1.rds.amazonaws.com:3306"
	RDSDatabase string `yaml:"rds_database"`
	RDSUser     string `yaml:"rds_user"`
	// RDSPassword provided via GOLDFISH_DB_PASSWORD environment variable

	// Common settings
	MaxConnections int `yaml:"max_connections"`
	MaxIdleTime    int `yaml:"max_idle_time_seconds"`
}
