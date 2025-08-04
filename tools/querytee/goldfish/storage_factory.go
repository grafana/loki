package goldfish

import (
	"fmt"
	"os"

	"github.com/go-kit/log"
)

// NewStorage creates a storage backend based on configuration
func NewStorage(config StorageConfig, logger log.Logger) (Storage, error) {
	switch config.Type {
	case "":
		// No storage configured, use no-op storage
		return NewNoOpStorage(logger), nil
	case "cloudsql":
		password := os.Getenv("GOLDFISH_DB_PASSWORD")
		return NewCloudSQLStorage(config, password)
	case "rds":
		password := os.Getenv("GOLDFISH_DB_PASSWORD")
		return NewRDSStorage(config, password)
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", config.Type)
	}
}
