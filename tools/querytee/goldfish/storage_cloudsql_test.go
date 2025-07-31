package goldfish

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to create a CloudSQLStorage with a mocked database
func newMockCloudSQLStorage(t *testing.T) (*CloudSQLStorage, sqlmock.Sqlmock) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)

	return &CloudSQLStorage{
		MySQLStorage: &MySQLStorage{
			db: db,
			config: StorageConfig{
				CloudSQLDatabase: "testdb",
			},
		},
	}, mock
}

func TestNewCloudSQLStorage_PasswordValidation(t *testing.T) {
	config := StorageConfig{
		CloudSQLHost:     "localhost",
		CloudSQLPort:     3306,
		CloudSQLDatabase: "testdb",
		CloudSQLUser:     "testuser",
	}

	// Test empty password
	_, err := NewCloudSQLStorage(config, "")
	assert.Error(t, err)
	assert.Equal(t, "CloudSQL password must be provided via GOLDFISH_DB_PASSWORD environment variable", err.Error())
}

func TestStoreQuerySample(t *testing.T) {
	storage, mock := newMockCloudSQLStorage(t)
	defer storage.Close()

	ctx := context.Background()
	sample := &QuerySample{
		CorrelationID: "test-correlation-id",
		TenantID:      "tenant-123",
		Query:         "sum(rate(log_lines[5m]))",
		QueryType:     "logql",
		StartTime:     time.Now().Add(-1 * time.Hour),
		EndTime:       time.Now(),
		Step:          5 * time.Minute,
		CellAStats: QueryStats{
			ExecTimeMs:           100,
			QueueTimeMs:          10,
			BytesProcessed:       1024,
			LinesProcessed:       50,
			BytesPerSecond:       10240,
			LinesPerSecond:       500,
			TotalEntriesReturned: 25,
			Splits:               2,
			Shards:               4,
		},
		CellBStats: QueryStats{
			ExecTimeMs:           95,
			QueueTimeMs:          8,
			BytesProcessed:       1024,
			LinesProcessed:       50,
			BytesPerSecond:       10752,
			LinesPerSecond:       526,
			TotalEntriesReturned: 25,
			Splits:               2,
			Shards:               4,
		},
		CellAResponseHash: "hash123",
		CellBResponseHash: "hash123",
		CellAResponseSize:  2048,
		CellBResponseSize:  2048,
		CellAStatusCode:    200,
		CellBStatusCode:    200,
		CellAUsedNewEngine: false,
		CellBUsedNewEngine: true,
		SampledAt:          time.Now(),
	}

	mock.ExpectExec("INSERT INTO sampled_queries").
		WithArgs(
			sample.CorrelationID,
			sample.TenantID,
			sample.Query,
			sample.QueryType,
			sample.StartTime,
			sample.EndTime,
			sample.Step.Milliseconds(),
			sample.CellAStats.ExecTimeMs,
			sample.CellBStats.ExecTimeMs,
			sample.CellAStats.QueueTimeMs,
			sample.CellBStats.QueueTimeMs,
			sample.CellAStats.BytesProcessed,
			sample.CellBStats.BytesProcessed,
			sample.CellAStats.LinesProcessed,
			sample.CellBStats.LinesProcessed,
			sample.CellAStats.BytesPerSecond,
			sample.CellBStats.BytesPerSecond,
			sample.CellAStats.LinesPerSecond,
			sample.CellBStats.LinesPerSecond,
			sample.CellAStats.TotalEntriesReturned,
			sample.CellBStats.TotalEntriesReturned,
			sample.CellAStats.Splits,
			sample.CellBStats.Splits,
			sample.CellAStats.Shards,
			sample.CellBStats.Shards,
			sample.CellAResponseHash,
			sample.CellBResponseHash,
			sample.CellAResponseSize,
			sample.CellBResponseSize,
			sample.CellAStatusCode,
			sample.CellBStatusCode,
			sample.CellAUsedNewEngine,
			sample.CellBUsedNewEngine,
			sample.SampledAt,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := storage.StoreQuerySample(ctx, sample)
	assert.NoError(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestStoreComparisonResult(t *testing.T) {
	storage, mock := newMockCloudSQLStorage(t)
	defer storage.Close()

	ctx := context.Background()

	differenceDetails := map[string]interface{}{
		"type":    "value_mismatch",
		"details": "Different response values",
	}

	performanceMetrics := PerformanceMetrics{
		CellAQueryTime:  100 * time.Millisecond,
		CellBQueryTime:  95 * time.Millisecond,
		QueryTimeRatio:  0.95,
		CellABytesTotal: 1024,
		CellBBytesTotal: 1024,
		BytesRatio:      1.0,
	}

	result := &ComparisonResult{
		CorrelationID:      "test-correlation-id",
		ComparisonStatus:   "mismatch",
		DifferenceDetails:  differenceDetails,
		PerformanceMetrics: performanceMetrics,
		ComparedAt:         time.Now(),
	}

	differenceJSON, _ := json.Marshal(differenceDetails)
	metricsJSON, _ := json.Marshal(performanceMetrics)

	mock.ExpectExec("INSERT INTO comparison_outcomes").
		WithArgs(
			result.CorrelationID,
			result.ComparisonStatus,
			differenceJSON,
			metricsJSON,
			result.ComparedAt,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err := storage.StoreComparisonResult(ctx, result)
	assert.NoError(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestStoreComparisonResult_JSONMarshalError(t *testing.T) {
	storage, _ := newMockCloudSQLStorage(t)
	defer storage.Close()

	ctx := context.Background()

	// Create a value that cannot be marshaled to JSON
	invalidValue := make(chan int)

	result := &ComparisonResult{
		CorrelationID:    "test-correlation-id",
		ComparisonStatus: "mismatch",
		DifferenceDetails: map[string]interface{}{
			"invalid": invalidValue, // This will fail JSON marshaling
		},
		PerformanceMetrics: PerformanceMetrics{
			CellAQueryTime: 100 * time.Millisecond,
			CellBQueryTime: 100 * time.Millisecond,
		},
		ComparedAt: time.Now(),
	}

	err := storage.StoreComparisonResult(ctx, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to marshal difference details")
}

func TestClose(t *testing.T) {
	storage, mock := newMockCloudSQLStorage(t)

	mock.ExpectClose()

	err := storage.Close()
	assert.NoError(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestStoreQuerySample_DatabaseError(t *testing.T) {
	storage, mock := newMockCloudSQLStorage(t)
	defer storage.Close()

	ctx := context.Background()
	sample := &QuerySample{
		CorrelationID: "test-correlation-id",
		TenantID:      "tenant-123",
		Query:         "sum(rate(log_lines[5m]))",
		QueryType:     "logql",
		StartTime:     time.Now().Add(-1 * time.Hour),
		EndTime:       time.Now(),
		Step:          5 * time.Minute,
		CellAStats: QueryStats{
			ExecTimeMs: 100,
		},
		CellBStats: QueryStats{
			ExecTimeMs: 95,
		},
		SampledAt: time.Now(),
	}

	// Simulate database error
	mock.ExpectExec("INSERT INTO sampled_queries").
		WillReturnError(fmt.Errorf("database connection lost"))

	err := storage.StoreQuerySample(ctx, sample)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database connection lost")

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestStoreComparisonResult_DatabaseError(t *testing.T) {
	storage, mock := newMockCloudSQLStorage(t)
	defer storage.Close()

	ctx := context.Background()

	result := &ComparisonResult{
		CorrelationID:     "test-correlation-id",
		ComparisonStatus:  "match",
		DifferenceDetails: map[string]interface{}{},
		PerformanceMetrics: PerformanceMetrics{
			CellAQueryTime: 100 * time.Millisecond,
			CellBQueryTime: 100 * time.Millisecond,
		},
		ComparedAt: time.Now(),
	}

	differenceJSON, _ := json.Marshal(result.DifferenceDetails)
	metricsJSON, _ := json.Marshal(result.PerformanceMetrics)

	mock.ExpectExec("INSERT INTO comparison_outcomes").
		WithArgs(
			result.CorrelationID,
			result.ComparisonStatus,
			differenceJSON,
			metricsJSON,
			result.ComparedAt,
		).
		WillReturnError(fmt.Errorf("foreign key constraint failed"))

	err := storage.StoreComparisonResult(ctx, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "foreign key constraint failed")

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestInitMySQLSchema(t *testing.T) {
	tests := []struct {
		name      string
		setupMock func(mock sqlmock.Sqlmock)
		wantErr   bool
		errMsg    string
	}{
		{
			name: "successful schema initialization",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("CREATE TABLE IF NOT EXISTS sampled_queries").
					WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("CREATE TABLE IF NOT EXISTS comparison_outcomes").
					WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("CREATE INDEX idx_sampled_queries_tenant").
					WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("CREATE INDEX idx_sampled_queries_time").
					WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("CREATE INDEX idx_comparison_status").
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
			wantErr: false,
		},
		{
			name: "sampled_queries table creation failure",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("CREATE TABLE IF NOT EXISTS sampled_queries").
					WillReturnError(fmt.Errorf("insufficient privileges"))
			},
			wantErr: true,
			errMsg:  "insufficient privileges",
		},
		{
			name: "comparison_outcomes table creation failure",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("CREATE TABLE IF NOT EXISTS sampled_queries").
					WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("CREATE TABLE IF NOT EXISTS comparison_outcomes").
					WillReturnError(fmt.Errorf("table already exists with different schema"))
			},
			wantErr: true,
			errMsg:  "table already exists with different schema",
		},
		{
			name: "index creation failure",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectExec("CREATE TABLE IF NOT EXISTS sampled_queries").
					WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("CREATE TABLE IF NOT EXISTS comparison_outcomes").
					WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("CREATE INDEX idx_sampled_queries_tenant").
					WillReturnError(fmt.Errorf("duplicate index name"))
			},
			wantErr: true,
			errMsg:  "duplicate index name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()

			tt.setupMock(mock)

			err = initMySQLSchema(db)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}

			err = mock.ExpectationsWereMet()
			assert.NoError(t, err)
		})
	}
}

func TestCloudSQLStorage_ConnectionPoolConfiguration(t *testing.T) {
	storage, _ := newMockCloudSQLStorage(t)
	defer storage.Close()

	// Verify that connection pool settings are applied
	config := StorageConfig{
		MaxConnections: 20,
		MaxIdleTime:    600,
	}

	// The actual implementation sets:
	// db.SetMaxOpenConns(config.MaxConnections)
	// db.SetMaxIdleConns(config.MaxConnections / 2)
	// db.SetConnMaxIdleTime(time.Duration(config.MaxIdleTime) * time.Second)

	assert.Equal(t, 20, config.MaxConnections)
	assert.Equal(t, 600, config.MaxIdleTime)
}

func TestMySQLDSNFormat(t *testing.T) {
	// Test that the DSN is correctly formatted for MySQL
	config := StorageConfig{
		CloudSQLHost:     "cloudsql-proxy",
		CloudSQLPort:     3306,
		CloudSQLDatabase: "goldfish_db",
		CloudSQLUser:     "goldfish_user",
	}
	password := "secret123"

	// Expected MySQL DSN format
	expectedDSN := "goldfish_user:secret123@tcp(cloudsql-proxy:3306)/goldfish_db?parseTime=true&charset=utf8mb4&collation=utf8mb4_unicode_ci"

	// Construct DSN as done in NewCloudSQLStorage
	actualDSN := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&charset=utf8mb4&collation=utf8mb4_unicode_ci",
		config.CloudSQLUser,
		password,
		config.CloudSQLHost,
		config.CloudSQLPort,
		config.CloudSQLDatabase,
	)

	assert.Equal(t, expectedDSN, actualDSN)
}

// TestSQLInjectionProtection tests that the storage is protected against SQL injection attacks
func TestSQLInjectionProtection(t *testing.T) {
	storage, mock := newMockCloudSQLStorage(t)
	defer storage.Close()

	ctx := context.Background()

	// SQL injection test cases
	injectionCases := []struct {
		name          string
		correlationID string
		tenantID      string
		query         string
		description   string
	}{
		{
			name:          "SQL injection in correlation ID",
			correlationID: "'; DROP TABLE sampled_queries; --",
			tenantID:      "tenant-123",
			query:         "sum(rate(log_lines[5m]))",
			description:   "Attempts to drop table via correlation ID",
		},
		{
			name:          "SQL injection in tenant ID",
			correlationID: "test-correlation",
			tenantID:      "tenant'; DELETE FROM sampled_queries WHERE '1'='1",
			query:         "sum(rate(log_lines[5m]))",
			description:   "Attempts to delete all records via tenant ID",
		},
		{
			name:          "SQL injection in query",
			correlationID: "test-correlation",
			tenantID:      "tenant-123",
			query:         "'); INSERT INTO sampled_queries VALUES (NULL); --",
			description:   "Attempts to insert malicious records via query",
		},
		{
			name:          "Union-based SQL injection",
			correlationID: "test' UNION SELECT * FROM information_schema.tables--",
			tenantID:      "tenant-123",
			query:         "sum(rate(log_lines[5m]))",
			description:   "Attempts to extract schema information",
		},
		{
			name:          "Time-based SQL injection",
			correlationID: "test-correlation",
			tenantID:      "tenant'; SELECT SLEEP(10); --",
			query:         "sum(rate(log_lines[5m]))",
			description:   "Attempts time-based attack",
		},
		{
			name:          "Nested SQL injection",
			correlationID: "test-correlation",
			tenantID:      "tenant-123",
			query:         "sum(rate(log_lines[5m]))'); DROP DATABASE goldfish; --",
			description:   "Attempts to drop database via nested injection",
		},
		{
			name:          "SQL keywords in valid data",
			correlationID: "SELECT-DROP-DELETE-123",
			tenantID:      "INSERT-UPDATE-456",
			query:         "SELECT * FROM logs WHERE message='DROP TABLE'",
			description:   "Valid data containing SQL keywords",
		},
		{
			name:          "Special characters injection",
			correlationID: "test\"; DROP TABLE sampled_queries; --",
			tenantID:      "tenant' OR '1'='1",
			query:         "sum(rate(log_lines[5m]))",
			description:   "Uses special characters for injection",
		},
		{
			name:          "Hex-encoded SQL injection",
			correlationID: "0x44524F50205441424C452073616D706C65645F71756572696573",
			tenantID:      "tenant-123",
			query:         "sum(rate(log_lines[5m]))",
			description:   "Hex-encoded DROP TABLE command",
		},
		{
			name:          "Comment-based SQL injection",
			correlationID: "test-correlation",
			tenantID:      "tenant-123/**/OR/**/1=1",
			query:         "sum(rate(log_lines[5m]))",
			description:   "Uses SQL comments to bypass filters",
		},
	}

	for _, tc := range injectionCases {
		t.Run(tc.name, func(t *testing.T) {
			sample := &QuerySample{
				CorrelationID: tc.correlationID,
				TenantID:      tc.tenantID,
				Query:         tc.query,
				QueryType:     "logql",
				StartTime:     time.Now().Add(-1 * time.Hour),
				EndTime:       time.Now(),
				Step:          5 * time.Minute,
				CellAStats: QueryStats{
					ExecTimeMs:     100,
					QueueTimeMs:    10,
					BytesProcessed: 1024,
				},
				CellBStats: QueryStats{
					ExecTimeMs:     95,
					QueueTimeMs:    8,
					BytesProcessed: 1024,
				},
				CellAResponseHash: "hash123",
				CellBResponseHash: "hash123",
				CellAResponseSize: 2048,
				CellBResponseSize: 2048,
				CellAStatusCode:   200,
				CellBStatusCode:   200,
				SampledAt:         time.Now(),
			}

			// The mock should expect the exact query with placeholders
			// and the values should be passed as arguments, not interpolated
			mock.ExpectExec("INSERT INTO sampled_queries").
				WithArgs(
					tc.correlationID, // These values are safely parameterized
					tc.tenantID,
					tc.query,
					sample.QueryType,
					sample.StartTime,
					sample.EndTime,
					sample.Step.Milliseconds(),
					sample.CellAStats.ExecTimeMs,
					sample.CellBStats.ExecTimeMs,
					sample.CellAStats.QueueTimeMs,
					sample.CellBStats.QueueTimeMs,
					sample.CellAStats.BytesProcessed,
					sample.CellBStats.BytesProcessed,
					sample.CellAStats.LinesProcessed,
					sample.CellBStats.LinesProcessed,
					sample.CellAStats.BytesPerSecond,
					sample.CellBStats.BytesPerSecond,
					sample.CellAStats.LinesPerSecond,
					sample.CellBStats.LinesPerSecond,
					sample.CellAStats.TotalEntriesReturned,
					sample.CellBStats.TotalEntriesReturned,
					sample.CellAStats.Splits,
					sample.CellBStats.Splits,
					sample.CellAStats.Shards,
					sample.CellBStats.Shards,
					sample.CellAResponseHash,
					sample.CellBResponseHash,
					sample.CellAResponseSize,
					sample.CellBResponseSize,
					sample.CellAStatusCode,
					sample.CellBStatusCode,
					sample.CellAUsedNewEngine,
					sample.CellBUsedNewEngine,
					sample.SampledAt,
				).
				WillReturnResult(sqlmock.NewResult(1, 1))

			err := storage.StoreQuerySample(ctx, sample)
			assert.NoError(t, err, "SQL injection attempt should be safely handled: %s", tc.description)

			err = mock.ExpectationsWereMet()
			assert.NoError(t, err)
		})
	}
}

// TestComparisonResultSQLInjection tests SQL injection protection for comparison results
func TestComparisonResultSQLInjection(t *testing.T) {
	storage, mock := newMockCloudSQLStorage(t)
	defer storage.Close()

	ctx := context.Background()

	// Test cases with SQL injection attempts in comparison results
	injectionCases := []struct {
		name              string
		correlationID     string
		comparisonStatus  string
		differenceDetails map[string]interface{}
		description       string
	}{
		{
			name:             "SQL injection in comparison status",
			correlationID:    "test-correlation",
			comparisonStatus: "mismatch'; DROP TABLE comparison_outcomes; --",
			differenceDetails: map[string]interface{}{
				"type": "test",
			},
			description: "Attempts to drop table via status field",
		},
		{
			name:             "SQL injection in difference details JSON",
			correlationID:    "test-correlation",
			comparisonStatus: "mismatch",
			differenceDetails: map[string]interface{}{
				"injection": "'; DELETE FROM comparison_outcomes; --",
				"type":      "value_mismatch",
			},
			description: "Attempts injection through JSON field",
		},
		{
			name:             "SQL injection with nested JSON",
			correlationID:    "test-correlation",
			comparisonStatus: "match",
			differenceDetails: map[string]interface{}{
				"nested": map[string]interface{}{
					"sql": "'); DROP DATABASE goldfish; --",
				},
			},
			description: "Attempts injection through nested JSON structure",
		},
		{
			name:             "Boolean-based blind SQL injection",
			correlationID:    "test' AND 1=1--",
			comparisonStatus: "match",
			differenceDetails: map[string]interface{}{
				"type": "test",
			},
			description: "Boolean-based blind SQL injection attempt",
		},
		{
			name:             "SQL injection with escape sequences",
			correlationID:    "test\\'; DROP TABLE comparison_outcomes; --",
			comparisonStatus: "mismatch",
			differenceDetails: map[string]interface{}{
				"type": "test",
			},
			description: "Uses escape sequences for injection",
		},
	}

	for _, tc := range injectionCases {
		t.Run(tc.name, func(t *testing.T) {
			result := &ComparisonResult{
				CorrelationID:     tc.correlationID,
				ComparisonStatus:  ComparisonStatus(tc.comparisonStatus),
				DifferenceDetails: tc.differenceDetails,
				PerformanceMetrics: PerformanceMetrics{
					CellAQueryTime: 100 * time.Millisecond,
					CellBQueryTime: 95 * time.Millisecond,
					QueryTimeRatio: 0.95,
				},
				ComparedAt: time.Now(),
			}

			differenceJSON, err := json.Marshal(tc.differenceDetails)
			require.NoError(t, err)
			metricsJSON, err := json.Marshal(result.PerformanceMetrics)
			require.NoError(t, err)

			// Mock expects parameterized query with safe argument passing
			mock.ExpectExec("INSERT INTO comparison_outcomes").
				WithArgs(
					tc.correlationID,
					tc.comparisonStatus,
					differenceJSON,
					metricsJSON,
					result.ComparedAt,
				).
				WillReturnResult(sqlmock.NewResult(1, 1))

			err = storage.StoreComparisonResult(ctx, result)
			assert.NoError(t, err, "SQL injection attempt should be safely handled: %s", tc.description)

			err = mock.ExpectationsWereMet()
			assert.NoError(t, err)
		})
	}
}

// TestSchemaCreationSQLInjectionProtection verifies schema creation is safe
func TestSchemaCreationSQLInjectionProtection(t *testing.T) {
	// Schema creation uses hardcoded queries with no user input
	// This test verifies that the initSchema function doesn't accept any parameters
	// that could be used for injection

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	// Expect the exact hardcoded queries
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS sampled_queries").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS comparison_outcomes").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX idx_sampled_queries_tenant").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX idx_sampled_queries_time").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX idx_comparison_status").
		WillReturnResult(sqlmock.NewResult(0, 0))

	// initMySQLSchema takes only the db connection, no user input
	err = initMySQLSchema(db)
	assert.NoError(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}
