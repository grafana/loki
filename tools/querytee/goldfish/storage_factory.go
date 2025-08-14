package goldfish

import (
	"fmt"

	"github.com/go-kit/log"
	"github.com/grafana/loki/v3/pkg/goldfish"
)

// NewStorage creates a storage backend based on configuration
func NewStorage(config StorageConfig, logger log.Logger) (goldfish.Storage, error) {
	// Convert to shared config format
	sharedConfig := goldfish.StorageConfig{
		Type:             config.Type,
		CloudSQLHost:     config.CloudSQLHost,
		CloudSQLPort:     config.CloudSQLPort,
		CloudSQLDatabase: config.CloudSQLDatabase,
		CloudSQLUser:     config.CloudSQLUser,
		RDSEndpoint:      config.RDSEndpoint,
		RDSDatabase:      config.RDSDatabase,
		RDSUser:          config.RDSUser,
		MaxConnections:   config.MaxConnections,
		MaxIdleTime:      config.MaxIdleTime,
	}

	switch config.Type {
	case "":
		// No storage configured, use no-op storage
		return goldfish.NewNoopStorage(), nil
	case "cloudsql", "rds":
		return goldfish.NewMySQLStorage(sharedConfig, logger)
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", config.Type)
	}
}
