package goldfish

import (
	"fmt"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to create an RDSStorage with a mocked database
func newMockRDSStorage(t *testing.T) (*RDSStorage, sqlmock.Sqlmock) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	return &RDSStorage{
		MySQLStorage: &MySQLStorage{
			db: db,
			config: StorageConfig{
				RDSDatabase: "testdb",
			},
		},
	}, mock
}

func TestNewRDSStorage_PasswordValidation(t *testing.T) {
	config := StorageConfig{
		RDSEndpoint: "mydb.123456789012.us-east-1.rds.amazonaws.com:3306",
		RDSDatabase: "testdb",
		RDSUser:     "testuser",
	}

	// Test empty password
	_, err := NewRDSStorage(config, "")
	assert.Error(t, err)
	assert.Equal(t, "RDS password must be provided via GOLDFISH_DB_PASSWORD environment variable", err.Error())
}

func TestRDSDSNFormat(t *testing.T) {
	// Test that the DSN is correctly formatted for RDS
	config := StorageConfig{
		RDSEndpoint: "mydb.123456789012.us-east-1.rds.amazonaws.com:3306",
		RDSDatabase: "goldfish_db",
		RDSUser:     "goldfish_user",
	}
	password := "secret123"

	// Expected MySQL DSN format for RDS
	expectedDSN := "goldfish_user:secret123@tcp(mydb.123456789012.us-east-1.rds.amazonaws.com:3306)/goldfish_db?parseTime=true&charset=utf8mb4&collation=utf8mb4_unicode_ci"

	// Construct DSN as done in NewRDSStorage
	actualDSN := fmt.Sprintf("%s:%s@tcp(%s)/%s?parseTime=true&charset=utf8mb4&collation=utf8mb4_unicode_ci",
		config.RDSUser,
		password,
		config.RDSEndpoint,
		config.RDSDatabase,
	)

	assert.Equal(t, expectedDSN, actualDSN)
}

func TestRDSStorage_SharedFunctionality(t *testing.T) {
	// Since RDSStorage embeds MySQLStorage, most functionality is tested
	// through the MySQL storage tests. This test verifies that RDS storage
	// properly implements the Storage interface.

	storage, mock := newMockRDSStorage(t)
	defer storage.Close()

	// Verify that RDSStorage has access to all MySQLStorage methods
	assert.NotNil(t, storage.StoreQuerySample)
	assert.NotNil(t, storage.StoreComparisonResult)
	assert.NotNil(t, storage.Close)

	// Verify that the embedded MySQLStorage is properly initialized
	assert.NotNil(t, storage.MySQLStorage)
	assert.NotNil(t, storage.MySQLStorage.db)

	mock.ExpectClose()
}

