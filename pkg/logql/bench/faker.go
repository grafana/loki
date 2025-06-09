package bench

import (
	"fmt"
	"math/rand"
	"time"
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
	}

	// New data for additional generators
	orgIDs = []string{"tenant1", "tenant2", "tenant3", "tenant4", "tenant5", "acme", "globex", "initech", "umbrella"}

	grpcMethods = []string{
		"/cortex.Ingester/Push",
		"/cortex.Querier/Query",
		"/cortex.Ruler/Rules",
		"/cortex.Distributor/Push",
		"/cortex.Compactor/Compact",
	}

	tempoComponents = []string{
		"distributor",
		"ingester",
		"querier",
		"compactor",
		"frontend",
	}

	tempoMessages = []string{
		"received traces",
		"querying traces",
		"compacting blocks",
		"flushing traces to storage",
		"handling request",
	}

	k8sComponents = []string{
		"kubelet",
		"kube-scheduler",
		"kube-controller-manager",
		"kube-apiserver",
		"etcd",
	}

	k8sLogPrefixes = []string{
		"I0612",
		"W0612",
		"E0612",
		"F0612",
	}

	k8sMessages = []string{
		"Started container",
		"Pulling image",
		"Created pod",
		"Scheduled pod",
		"Node status updated",
		"Volume mounted",
		"Service endpoint updated",
	}

	prometheusComponents = []string{
		"tsdb",
		"scrape",
		"rules",
		"remote",
		"web",
	}

	prometheusSubcomponents = []string{
		"manager",
		"head",
		"wal",
		"api",
		"discovery",
	}

	prometheusMessages = []string{
		"Compacting blocks",
		"Scraping target",
		"Evaluating rules",
		"Remote write",
		"Handling request",
		"WAL replay complete",
	}

	grafanaLoggers = []string{
		"http.server",
		"alerting",
		"auth",
		"datasources",
		"rendering",
	}

	grafanaComponents = []string{
		"server",
		"api",
		"sqlstore",
		"plugins",
		"auth",
	}

	grafanaMessages = []string{
		"Request completed",
		"User logged in",
		"Dashboard saved",
		"Alert rule evaluated",
		"Data source health check",
		"Plugin loaded",
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

// Duration returns a random duration
func (f *Faker) Duration() time.Duration {
	return time.Duration(f.rnd.Intn(1000)+1) * time.Millisecond // 1-1000ms
}

// UserAgent returns a random user agent string
func (f *Faker) UserAgent() string {
	return userAgents[f.rnd.Intn(len(userAgents))]
}

// IP returns a random IP address
func (f *Faker) IP() string {
	return fmt.Sprintf("%d.%d.%d.%d", f.rnd.Intn(256), f.rnd.Intn(256), f.rnd.Intn(256), f.rnd.Intn(256))
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

// RowsAffected returns a random number of rows affected
func (f *Faker) RowsAffected() int {
	return f.rnd.Intn(100) + 1 // 1-100 rows
}

// CacheOp returns a random cache operation
func (f *Faker) CacheOp() string {
	return cacheOps[f.rnd.Intn(len(cacheOps))]
}

// CacheKey returns a random cache key
func (f *Faker) CacheKey() string {
	return fmt.Sprintf("key:%d", f.rnd.Intn(1000))
}

// CacheSize returns a random cache size in bytes
func (f *Faker) CacheSize() int {
	return f.rnd.Intn(10000) + 1 // 1-10000 bytes
}

// CacheTTL returns a random cache TTL in seconds
func (f *Faker) CacheTTL() int {
	return f.rnd.Intn(3600) + 60 // 60-3660 seconds
}

// AuthAction returns a random authentication action
func (f *Faker) AuthAction() string {
	return authActions[f.rnd.Intn(len(authActions))]
}

// User returns a random username
func (f *Faker) User() string {
	return fmt.Sprintf("user%d", f.rnd.Intn(1000))
}

// AuthSuccess returns a random authentication success status
func (f *Faker) AuthSuccess() bool {
	return f.rnd.Float32() < 0.8 // 80% success rate
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

// New methods for additional generators

// OrgID returns a random organization ID
func (f *Faker) OrgID() string {
	return orgIDs[f.rnd.Intn(len(orgIDs))]
}

// GRPCMethod returns a random gRPC method
func (f *Faker) GRPCMethod() string {
	return grpcMethods[f.rnd.Intn(len(grpcMethods))]
}

// ErrorMessage returns a random error message
func (f *Faker) ErrorMessage() string {
	return errorMessages[f.rnd.Intn(len(errorMessages))]
}

// TempoComponent returns a random Tempo component
func (f *Faker) TempoComponent() string {
	return tempoComponents[f.rnd.Intn(len(tempoComponents))]
}

// TempoMessage returns a random Tempo message
func (f *Faker) TempoMessage() string {
	return tempoMessages[f.rnd.Intn(len(tempoMessages))]
}

// K8sComponent returns a random Kubernetes component
func (f *Faker) K8sComponent() string {
	return k8sComponents[f.rnd.Intn(len(k8sComponents))]
}

// K8sLogPrefix returns a random Kubernetes log prefix
func (f *Faker) K8sLogPrefix() string {
	return k8sLogPrefixes[f.rnd.Intn(len(k8sLogPrefixes))]
}

// K8sMessage returns a random Kubernetes message
func (f *Faker) K8sMessage() string {
	return k8sMessages[f.rnd.Intn(len(k8sMessages))]
}

// PrometheusComponent returns a random Prometheus component
func (f *Faker) PrometheusComponent() string {
	return prometheusComponents[f.rnd.Intn(len(prometheusComponents))]
}

// PrometheusSubcomponent returns a random Prometheus subcomponent
func (f *Faker) PrometheusSubcomponent() string {
	return prometheusSubcomponents[f.rnd.Intn(len(prometheusSubcomponents))]
}

// PrometheusMessage returns a random Prometheus message
func (f *Faker) PrometheusMessage() string {
	return prometheusMessages[f.rnd.Intn(len(prometheusMessages))]
}

// GrafanaLogger returns a random Grafana logger
func (f *Faker) GrafanaLogger() string {
	return grafanaLoggers[f.rnd.Intn(len(grafanaLoggers))]
}

// GrafanaComponent returns a random Grafana component
func (f *Faker) GrafanaComponent() string {
	return grafanaComponents[f.rnd.Intn(len(grafanaComponents))]
}

// GrafanaMessage returns a random Grafana message
func (f *Faker) GrafanaMessage() string {
	return grafanaMessages[f.rnd.Intn(len(grafanaMessages))]
}

// LogGenerator is a function that generates a log line
type LogGenerator func(level string, timestamp time.Time, faker *Faker) string

// Application represents a type of application that generates logs
type Application struct {
	Name         string
	LogGenerator LogGenerator
	OTELResource map[string]string // OTEL resource attributes
}

// Register standard application types with known log patterns
var defaultApplications = []Application{
	{
		Name: "web-server",
		LogGenerator: func(level string, ts time.Time, f *Faker) string {
			// JSON format with variations
			baseJSON := fmt.Sprintf(
				`{"level":"%s","ts":"%s","msg":"HTTP request","method":"%s","path":"%s","status":%d,"duration":%d,"user_agent":"%s","client_ip":"%s"`,
				level, ts.Format(time.RFC3339), f.Method(), f.Path(), f.Status(), f.Duration().Milliseconds(), f.UserAgent(), f.IP(),
			)

			// Sometimes add request ID
			if f.rnd.Float32() < 0.7 {
				baseJSON += fmt.Sprintf(`,"request_id":"%s"`, f.TraceID())
			}

			// Sometimes add user info
			if f.rnd.Float32() < 0.4 {
				baseJSON += fmt.Sprintf(`,"user":"%s"`, f.User())
			}

			// Sometimes add error info for non-200 status codes
			if f.Status() >= 400 {
				baseJSON += fmt.Sprintf(`,"error":"%s"`, f.ErrorMessage())
			}

			return baseJSON + "}"
		},
		OTELResource: map[string]string{
			"service_name":           "web-server",
			"service_version":        "1.0.0",
			"service_namespace":      "default",
			"telemetry_sdk_name":     "opentelemetry",
			"telemetry_sdk_language": "go",
			"deployment_environment": "production",
		},
	},
	{
		Name: "database",
		LogGenerator: func(level string, ts time.Time, f *Faker) string {
			// JSON format with variations
			baseJSON := fmt.Sprintf(
				`{"level":"%s","ts":"%s","msg":"Query executed","query_type":"%s","table":"%s","duration":%d,"rows_affected":%d`,
				level, ts.Format(time.RFC3339), f.QueryType(), f.Table(), f.Duration().Milliseconds(), f.RowsAffected(),
			)

			// Sometimes add query ID
			if f.rnd.Float32() < 0.6 {
				baseJSON += fmt.Sprintf(`,"query_id":"%s"`, f.TraceID())
			}

			// Sometimes add user info
			if f.rnd.Float32() < 0.3 {
				baseJSON += fmt.Sprintf(`,"user":"%s"`, f.User())
			}

			// Add error for error level logs
			if level == errorLevel {
				baseJSON += fmt.Sprintf(`,"error":"%s"`, f.ErrorMessage())
			}

			return baseJSON + "}"
		},
		OTELResource: map[string]string{
			"service_name":    "mysql",
			"service_version": "8.0.28",
			"db_system":       "mysql",
			"db_version":      "8.0.28",
			"db_instance":     "primary",
			"db_cluster":      "production",
		},
	},
	{
		Name: "cache",
		LogGenerator: func(level string, ts time.Time, f *Faker) string {
			// JSON format with variations
			baseJSON := fmt.Sprintf(
				`{"level":"%s","ts":"%s","msg":"Cache operation","operation":"%s","key":"%s","size":%d,"ttl":%d`,
				level, ts.Format(time.RFC3339), f.CacheOp(), f.CacheKey(), f.CacheSize(), f.CacheTTL(),
			)

			// Sometimes add request ID
			if f.rnd.Float32() < 0.5 {
				baseJSON += fmt.Sprintf(`,"request_id":"%s"`, f.TraceID())
			}

			// Sometimes add latency
			if f.rnd.Float32() < 0.7 {
				baseJSON += fmt.Sprintf(`,"latency":"%s"`, f.Duration())
			}

			// Add error for error level logs
			if level == errorLevel {
				baseJSON += fmt.Sprintf(`,"error":"%s"`, f.CacheError())
			}

			return baseJSON + "}"
		},
		OTELResource: map[string]string{
			"service_name":     "redis",
			"service_version":  "6.2.6",
			"redis_cluster":    "false",
			"redis_db":         "0",
			"redis_max_memory": "17179869184",
		},
	},
	{
		Name: "auth-service",
		LogGenerator: func(level string, ts time.Time, f *Faker) string {
			// JSON format with variations
			baseJSON := fmt.Sprintf(
				`{"level":"%s","ts":"%s","msg":"Authentication request","action":"%s","user":"%s","success":%t,"duration":"%s"`,
				level, ts.Format(time.RFC3339), f.AuthAction(), f.User(), f.AuthSuccess(), f.Duration(),
			)

			// Sometimes add user info
			if f.rnd.Float32() < 0.8 {
				baseJSON += fmt.Sprintf(`,"user_details":"%s"`, f.User())
			}

			// Sometimes add client IP
			if f.rnd.Float32() < 0.7 {
				baseJSON += fmt.Sprintf(`,"client_ip":"%s"`, f.IP())
			}

			// Sometimes add request ID
			if f.rnd.Float32() < 0.6 {
				baseJSON += fmt.Sprintf(`,"request_id":"%s"`, f.TraceID())
			}

			// Add error for error level logs or failed auth
			if level == errorLevel || !f.AuthSuccess() {
				baseJSON += fmt.Sprintf(`,"error":"%s"`, f.AuthError())
			}

			return baseJSON + "}"
		},
		OTELResource: map[string]string{
			"service_name":    "auth-service",
			"service_version": "1.5.2",
			"auth_provider":   "oauth2",
			"auth_methods":    "password,token,sso",
		},
	},
	{
		Name: "kafka",
		LogGenerator: func(level string, ts time.Time, f *Faker) string {
			// JSON format with variations
			baseJSON := fmt.Sprintf(
				`{"level":"%s","ts":"%s","msg":"Kafka event","topic":"%s","partition":%d,"offset":%d,"event":"%s"`,
				level, ts.Format(time.RFC3339), f.KafkaTopic(), f.KafkaPartition(), f.KafkaOffset(), f.KafkaEvent(),
			)

			// Sometimes add message size
			if f.rnd.Float32() < 0.7 {
				baseJSON += fmt.Sprintf(`,"size":%d`, f.rnd.Intn(10000))
			}

			// Sometimes add latency
			if f.rnd.Float32() < 0.6 {
				baseJSON += fmt.Sprintf(`,"latency":"%s"`, f.Duration())
			}

			// Sometimes add client info
			if f.rnd.Float32() < 0.5 {
				baseJSON += fmt.Sprintf(`,"client":"%s"`, f.Hostname())
			}

			// Add error for error level logs
			if level == errorLevel {
				baseJSON += fmt.Sprintf(`,"error":"%s"`, f.KafkaError())
			}

			return baseJSON + "}"
		},
		OTELResource: map[string]string{
			"service_name":    "kafka",
			"service_version": "3.4.0",
			"broker_id":       "${BROKER_ID}",
			"cluster_id":      "kafka-prod-01",
			"num_partitions":  "24",
		},
	},
	{
		Name: "nginx",
		LogGenerator: func(_ string, ts time.Time, f *Faker) string {
			return fmt.Sprintf(
				`%s - %s [%s] "%s %s HTTP/1.1" %d %d "%s" "%s"`,
				f.IP(), f.User(), ts.Format("02/Jan/2006:15:04:05 -0700"),
				f.Method(), f.NginxPath(), f.Status(), f.rnd.Intn(10000),
				f.Referer(), f.UserAgent(),
			)
		},
		OTELResource: map[string]string{
			"service_name":    "nginx",
			"service_version": "1.22.1",
			"hostname":        "${HOSTNAME}",
		},
	},
	{
		Name: "syslog",
		LogGenerator: func(_ string, _ time.Time, f *Faker) string {
			return fmt.Sprintf(
				`<%d>%s %s[%d]: %s`,
				f.SyslogPriority(false), f.Hostname(), "systemd", f.PID(),
				"Starting service...",
			)
		},
		OTELResource: map[string]string{
			"service_name": "system",
			"hostname":     "${HOSTNAME}",
			"os_type":      "linux",
			"os_version":   "Ubuntu 22.04",
		},
	},
	// New applications with logfmt and other formats
	{
		Name: "mimir",
		LogGenerator: func(level string, ts time.Time, f *Faker) string {
			// Logfmt format for Mimir
			baseLogfmt := fmt.Sprintf(
				`level=%s ts=%s caller=grpc_logging.go:%d msg="gRPC request" method=%s`,
				level, ts.Format(time.RFC3339Nano), 50+f.rnd.Intn(100), f.GRPCMethod(),
			)

			// Sometimes add tenant ID
			if f.rnd.Float32() < 0.7 {
				baseLogfmt += fmt.Sprintf(` tenant=%s`, f.OrgID())
			}

			// Sometimes add duration
			if f.rnd.Float32() < 0.8 {
				baseLogfmt += fmt.Sprintf(` duration=%s`, f.Duration())
			}

			// Sometimes add trace info
			if f.rnd.Float32() < 0.4 {
				baseLogfmt += fmt.Sprintf(` trace_id=%s span_id=%s`, f.TraceID(), f.SpanID())
			}

			// Add error for error level logs
			if level == errorLevel {
				baseLogfmt += fmt.Sprintf(` error="failed to %s: %s"`, f.GRPCMethod(), f.ErrorMessage())
			}

			return baseLogfmt
		},
		OTELResource: map[string]string{
			"service_name":    "mimir",
			"service_version": "2.8.0",
			"hostname":        "${HOSTNAME}",
			"cluster":         "prod-metrics",
		},
	},
	{
		Name: "loki",
		LogGenerator: func(level string, ts time.Time, f *Faker) string {
			// Logfmt format for Loki
			baseLogfmt := fmt.Sprintf(
				`level=%s ts=%s caller=ingester.go:%d msg="ingester request" component=ingester`,
				level, ts.Format(time.RFC3339Nano), 100+f.rnd.Intn(900),
			)

			// Sometimes add tenant ID
			if f.rnd.Float32() < 0.7 {
				baseLogfmt += fmt.Sprintf(` org_id=%s`, f.OrgID())
			}

			// Sometimes add duration
			if f.rnd.Float32() < 0.6 {
				baseLogfmt += fmt.Sprintf(` duration=%s`, f.Duration())
			}

			// Sometimes add trace info
			if f.rnd.Float32() < 0.4 {
				baseLogfmt += fmt.Sprintf(` trace_id=%s span_id=%s`, f.TraceID(), f.SpanID())
			}

			// Add metrics for some logs
			if f.rnd.Float32() < 0.5 {
				baseLogfmt += fmt.Sprintf(` streams=%d bytes=%d`, f.rnd.Intn(1000), f.rnd.Intn(10000000))
			}

			// Add error for error level logs
			if level == errorLevel {
				baseLogfmt += fmt.Sprintf(` error="failed to process request: %s"`, f.ErrorMessage())
			}

			return baseLogfmt
		},
		OTELResource: map[string]string{
			"service_name":    "loki",
			"service_version": "2.9.0",
			"hostname":        "${HOSTNAME}",
			"cluster":         "prod-logs",
		},
	},
	{
		Name: "tempo",
		LogGenerator: func(level string, ts time.Time, f *Faker) string {
			// Logfmt format for Tempo
			baseLogfmt := fmt.Sprintf(
				`level=%s ts=%s caller=distributor.go:%d msg="distributor request" component=distributor`,
				level, ts.Format(time.RFC3339Nano), 100+f.rnd.Intn(900),
			)

			// Sometimes add tenant ID
			if f.rnd.Float32() < 0.7 {
				baseLogfmt += fmt.Sprintf(` org_id=%s`, f.OrgID())
			}

			// Sometimes add duration
			if f.rnd.Float32() < 0.6 {
				baseLogfmt += fmt.Sprintf(` duration=%s`, f.Duration())
			}

			// Sometimes add trace info
			if f.rnd.Float32() < 0.8 {
				baseLogfmt += fmt.Sprintf(` trace_id=%s span_id=%s`, f.TraceID(), f.SpanID())
			}

			// Add metrics for some logs
			if f.rnd.Float32() < 0.5 {
				baseLogfmt += fmt.Sprintf(` spans=%d bytes=%d`, f.rnd.Intn(1000), f.rnd.Intn(10000000))
			}

			// Add error for error level logs
			if level == errorLevel {
				baseLogfmt += fmt.Sprintf(` error="failed to process trace: %s"`, f.ErrorMessage())
			}

			return baseLogfmt
		},
		OTELResource: map[string]string{
			"service_name":    "tempo",
			"service_version": "2.1.0",
			"hostname":        "${HOSTNAME}",
			"cluster":         "prod-traces",
		},
	},
	{
		Name: "kubernetes",
		LogGenerator: func(level string, ts time.Time, f *Faker) string {
			// Kubernetes log format (mix of structured and unstructured)
			component := f.K8sComponent()
			return fmt.Sprintf(
				`%s %s [%s] %s: %s`,
				ts.Format("2006-01-02T15:04:05.000000Z"), level, component,
				f.K8sLogPrefix(), f.K8sMessage(),
			)
		},
		OTELResource: map[string]string{
			"service_name":    "kubernetes",
			"service_version": "1.26.3",
			"hostname":        "${HOSTNAME}",
			"node_name":       "${HOSTNAME}",
			"cluster":         "prod-k8s",
		},
	},
	{
		Name: "prometheus",
		LogGenerator: func(level string, ts time.Time, f *Faker) string {
			// JSON format with variations
			baseJSON := fmt.Sprintf(
				`{"level":"%s","ts":"%s","caller":"%s:%d","component":"%s","msg":"%s"`,
				level, ts.Format(time.RFC3339Nano), f.PrometheusComponent(), 100+f.rnd.Intn(900),
				f.PrometheusSubcomponent(), f.PrometheusMessage(),
			)

			// Sometimes add duration
			if f.rnd.Float32() < 0.7 {
				baseJSON += fmt.Sprintf(`,"duration":"%s"`, f.Duration())
			}

			// Sometimes add metrics
			if f.rnd.Float32() < 0.5 {
				baseJSON += fmt.Sprintf(`,"samples":%d,"series":%d`,
					f.rnd.Intn(100000), f.rnd.Intn(10000))
			}

			// Add error for error level logs
			if level == errorLevel {
				baseJSON += fmt.Sprintf(`,"error":"%s"`, f.ErrorMessage())
			}

			return baseJSON + "}"
		},
		OTELResource: map[string]string{
			"service_name":    "prometheus",
			"service_version": "2.43.0",
			"hostname":        "${HOSTNAME}",
			"cluster":         "prod-monitoring",
		},
	},
	{
		Name: "grafana",
		LogGenerator: func(level string, ts time.Time, f *Faker) string {
			// Logfmt format for Grafana
			baseLogfmt := fmt.Sprintf(
				`level=%s ts=%s logger=%s caller=%s:%d msg="%s"`,
				level, ts.Format(time.RFC3339Nano), f.GrafanaLogger(), f.GrafanaComponent(), 100+f.rnd.Intn(900),
				f.GrafanaMessage(),
			)

			// Add user and org info
			baseLogfmt += fmt.Sprintf(` userId=%d orgId=%s`, f.rnd.Intn(1000), f.OrgID())

			// Sometimes add duration
			if f.rnd.Float32() < 0.7 {
				baseLogfmt += fmt.Sprintf(` duration=%s`, f.Duration())
			}

			// Sometimes add request info
			if f.rnd.Float32() < 0.5 {
				baseLogfmt += fmt.Sprintf(` method=%s path="%s" status=%d`,
					f.Method(), f.Path(), f.Status())
			}

			// Add error for error level logs
			if level == errorLevel {
				baseLogfmt += fmt.Sprintf(` error="%s"`, f.ErrorMessage())
			}

			return baseLogfmt
		},
		OTELResource: map[string]string{
			"service_name":    "grafana",
			"service_version": "10.0.3",
			"hostname":        "${HOSTNAME}",
			"cluster":         "prod-dashboards",
		},
	},
}
