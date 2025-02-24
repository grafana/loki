package bench

import (
	"fmt"
	"math/rand"
)

// Data for generating log entries
var (
	httpMethods = []string{"GET", "POST", "PUT", "DELETE", "PATCH"}
	apiPaths    = []string{
		"/api/v1/users",
		"/api/v1/products",
		"/api/v1/orders",
		"/api/v1/auth/login",
		"/api/v1/auth/logout",
		"/api/v2/metrics",
		"/api/v2/logs",
		"/healthz",
		"/metrics",
	}
	httpStatus = []int{200, 201, 204, 301, 302, 400, 401, 403, 404, 500, 503}
	userAgents = []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 14_7_1 like Mac OS X) AppleWebKit/605.1.15",
		"Mozilla/5.0 (Linux; Android 11; SM-G991B) AppleWebKit/537.36",
		"curl/7.64.1",
		"Apache-HttpClient/4.5.13",
		"python-requests/2.26.0",
	}
	queryTypes  = []string{"SELECT", "INSERT", "UPDATE", "DELETE", "MERGE"}
	dbTables    = []string{"users", "products", "orders", "sessions", "logs", "metrics"}
	cacheOps    = []string{"get", "set", "delete", "expire", "flush"}
	authActions = []string{"login", "logout", "password_reset", "token_refresh", "permission_check"}

	errorMessages = []string{
		"Invalid request parameters",
		"Unauthorized access",
		"Resource not found",
		"Internal server error",
		"Service unavailable",
		"Rate limit exceeded",
		"Invalid content type",
	}
	dbErrors = []string{
		"Connection refused",
		"Deadlock detected",
		"Unique constraint violation",
		"Foreign key constraint violation",
		"Query timeout",
		"Table does not exist",
	}
	cacheErrors = []string{
		"Connection refused",
		"Key not found",
		"Invalid key format",
		"Memory limit exceeded",
		"Serialization error",
	}
	authErrors = []string{
		"Invalid credentials",
		"Account locked",
		"Session expired",
		"Invalid token",
		"Too many attempts",
		"Password expired",
	}
	nginxPaths = []string{
		"/",
		"/api/",
		"/static/",
		"/images/",
		"/css/",
		"/js/",
		"/upload/",
		"/download/",
		"/admin/",
		"/auth/",
	}
	nginxErrorTypes = []string{
		"access forbidden",
		"client closed connection",
		"upstream timed out",
		"file not found",
		"invalid request",
	}
	nginxErrors = []string{
		"access forbidden by rule",
		"client closed connection while reading request headers",
		"upstream timed out (110: Connection timed out)",
		"file not found",
		"client sent invalid request",
	}
	kafkaTopics = []string{
		"users",
		"orders",
		"payments",
		"notifications",
		"logs",
		"metrics",
		"events",
	}
	kafkaEvents = []string{
		"producer_send",
		"consumer_fetch",
		"partition_assignment",
		"replication_completed",
		"leader_election",
	}
	kafkaErrors = []string{
		"Leader not available",
		"Network connection failure",
		"Topic authorization failed",
		"Record too large",
		"Offset out of range",
	}
)

// Faker provides methods to generate fake data consistently
type Faker struct {
	rnd *rand.Rand
}

// NewFaker creates a new faker with the given random source
func NewFaker(rnd *rand.Rand) *Faker {
	return &Faker{rnd: rnd}
}

// Method returns a random HTTP method
func (f *Faker) Method() string {
	return httpMethods[f.rnd.Intn(len(httpMethods))]
}

// Path returns a random API path
func (f *Faker) Path() string {
	return apiPaths[f.rnd.Intn(len(apiPaths))]
}

// Status returns a random HTTP status code
func (f *Faker) Status() int {
	return httpStatus[f.rnd.Intn(len(httpStatus))]
}

// UserAgent returns a random user agent string
func (f *Faker) UserAgent() string {
	return userAgents[f.rnd.Intn(len(userAgents))]
}

// IP returns a random IP address
func (f *Faker) IP() string {
	return fmt.Sprintf("%d.%d.%d.%d",
		f.rnd.Intn(256), f.rnd.Intn(256), f.rnd.Intn(256), f.rnd.Intn(256))
}

// TraceID returns a random trace ID
func (f *Faker) TraceID() string {
	b := make([]byte, 16)
	f.rnd.Read(b)
	return fmt.Sprintf("%x", b)
}

// SpanID returns a random span ID
func (f *Faker) SpanID() string {
	b := make([]byte, 8)
	f.rnd.Read(b)
	return fmt.Sprintf("%x", b)
}

// QueryType returns a random database query type
func (f *Faker) QueryType() string {
	return queryTypes[f.rnd.Intn(len(queryTypes))]
}

// Table returns a random database table name
func (f *Faker) Table() string {
	return dbTables[f.rnd.Intn(len(dbTables))]
}

// Duration returns a random duration in milliseconds
func (f *Faker) Duration() int {
	return f.rnd.Intn(1000)
}

// RowsAffected returns a random number of rows affected
func (f *Faker) RowsAffected() int {
	return f.rnd.Intn(1000)
}

// CacheOp returns a random cache operation
func (f *Faker) CacheOp() string {
	return cacheOps[f.rnd.Intn(len(cacheOps))]
}

// CacheKey returns a random cache key
func (f *Faker) CacheKey() string {
	return fmt.Sprintf("key-%d", f.rnd.Intn(1000))
}

// CacheSize returns a random cache size
func (f *Faker) CacheSize() int {
	return f.rnd.Intn(10000)
}

// CacheTTL returns a random cache TTL in seconds
func (f *Faker) CacheTTL() int {
	return f.rnd.Intn(3600)
}

// AuthAction returns a random auth action
func (f *Faker) AuthAction() string {
	return authActions[f.rnd.Intn(len(authActions))]
}

// User returns a random username
func (f *Faker) User() string {
	return fmt.Sprintf("user-%d", f.rnd.Intn(1000))
}

// AuthSuccess returns a random authentication success boolean
// with a bias toward successful auth (90%)
func (f *Faker) AuthSuccess() bool {
	return f.rnd.Float32() > 0.1
}

// Error returns a random general error message
func (f *Faker) Error() string {
	return errorMessages[f.rnd.Intn(len(errorMessages))]
}

// DBError returns a random database error
func (f *Faker) DBError() string {
	return dbErrors[f.rnd.Intn(len(dbErrors))]
}

// CacheError returns a random cache error
func (f *Faker) CacheError() string {
	return cacheErrors[f.rnd.Intn(len(cacheErrors))]
}

// AuthError returns a random authentication error
func (f *Faker) AuthError() string {
	return authErrors[f.rnd.Intn(len(authErrors))]
}

// KafkaTopic returns a random Kafka topic
func (f *Faker) KafkaTopic() string {
	return kafkaTopics[f.rnd.Intn(len(kafkaTopics))]
}

// KafkaEvent returns a random Kafka event
func (f *Faker) KafkaEvent() string {
	return kafkaEvents[f.rnd.Intn(len(kafkaEvents))]
}

// KafkaPartition returns a random Kafka partition
func (f *Faker) KafkaPartition() int {
	return f.rnd.Intn(10)
}

// KafkaOffset returns a random Kafka offset
func (f *Faker) KafkaOffset() int {
	return f.rnd.Intn(100000)
}

// KafkaError returns a random Kafka error
func (f *Faker) KafkaError() string {
	return kafkaErrors[f.rnd.Intn(len(kafkaErrors))]
}

// NginxPath returns a random nginx path
func (f *Faker) NginxPath() string {
	return nginxPaths[f.rnd.Intn(len(nginxPaths))]
}

// NginxErrorType returns a random nginx error type
func (f *Faker) NginxErrorType() string {
	return nginxErrorTypes[f.rnd.Intn(len(nginxErrorTypes))]
}

// Hostname returns a random hostname
func (f *Faker) Hostname() string {
	return fmt.Sprintf("host-%d", f.rnd.Intn(100))
}

// Referer returns a random referer URL
func (f *Faker) Referer() string {
	referers := []string{
		"https://grafana.com",
		"https://github.com",
		"https://google.com",
		"https://example.com",
	}
	return referers[f.rnd.Intn(len(referers))]
}

// SyslogPriority returns a random syslog priority
func (f *Faker) SyslogPriority(isError bool) int {
	if isError {
		return 11 + f.rnd.Intn(4) // Error priority
	}
	return 14 + f.rnd.Intn(10) // Normal priority
}

// PID returns a random process ID
func (f *Faker) PID() int {
	return f.rnd.Intn(10000)
}
