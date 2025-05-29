package goldfish

import (
	"fmt"
)

// NewStorage creates a storage backend based on configuration
func NewStorage(config StorageConfig) (Storage, error) {
	switch config.Type {
	case "cloudsql":
		// TODO: Update CloudSQL implementation and auth/config
		return NewCloudSQLStorage(config)
	case "bigquery":
		// TODO: Implement BigQuery storage
		return nil, fmt.Errorf("BigQuery storage not yet implemented")
	case "postgres", "mysql":
		// TODO: Implement generic SQL storage using DSN
		return nil, fmt.Errorf("generic SQL storage not yet implemented")
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", config.Type)
	}
}
