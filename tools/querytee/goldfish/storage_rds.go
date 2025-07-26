package goldfish

import (
	"fmt"
)

// RDSStorage implements Storage for Amazon RDS MySQL
type RDSStorage struct {
	*MySQLStorage
}

// NewRDSStorage creates a new RDS storage backend
func NewRDSStorage(config StorageConfig, password string) (*RDSStorage, error) {
	if password == "" {
		return nil, fmt.Errorf("RDS password must be provided via GOLDFISH_DB_PASSWORD environment variable")
	}

	// Build DSN for RDS connection
	// MySQL DSN format: username:password@tcp(endpoint)/dbname?params
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?parseTime=true&charset=utf8mb4&collation=utf8mb4_unicode_ci",
		config.RDSUser,
		password,
		config.RDSEndpoint,
		config.RDSDatabase,
	)

	mysqlStorage, err := NewMySQLStorage(dsn, config)
	if err != nil {
		return nil, err
	}

	return &RDSStorage{
		MySQLStorage: mysqlStorage,
	}, nil
}

