package goldfish

import (
	"fmt"
)

// CloudSQLStorage implements Storage for Google Cloud SQL
type CloudSQLStorage struct {
	*MySQLStorage
}

// NewCloudSQLStorage creates a new CloudSQL storage backend
func NewCloudSQLStorage(config StorageConfig, password string) (*CloudSQLStorage, error) {
	if password == "" {
		return nil, fmt.Errorf("CloudSQL password must be provided via GOLDFISH_DB_PASSWORD environment variable")
	}

	// Build DSN for CloudSQL proxy connection
	// MySQL DSN format: username:password@tcp(host:port)/dbname?params
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&charset=utf8mb4&collation=utf8mb4_unicode_ci",
		config.CloudSQLUser,
		password,
		config.CloudSQLHost,
		config.CloudSQLPort,
		config.CloudSQLDatabase,
	)

	mysqlStorage, err := NewMySQLStorage(dsn, config)
	if err != nil {
		return nil, err
	}

	return &CloudSQLStorage{
		MySQLStorage: mysqlStorage,
	}, nil
}
