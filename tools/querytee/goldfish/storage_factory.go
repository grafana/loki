package goldfish

import (
	"fmt"

	"github.com/go-kit/log"
)

// NewStorage creates a storage backend based on configuration
func NewStorage(config StorageConfig, logger log.Logger) (Storage, error) {
	switch config.Type {
	case "":
		// No storage configured, use no-op storage
		return NewNoOpStorage(logger), nil
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
