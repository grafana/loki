// +build requires_docker

package e2e

import (
	"fmt"
	"html/template"
	"os"
	"strings"

	e2edb "github.com/cortexproject/cortex/integration/e2e/db"
)

const (
	defaultNetworkName = "loki-e2e-test"
	bucketName         = "loki-e2e-test-bucket"
)

// getNetworkName returns the docker network name to run tests in.
func getNetworkName() string {
	// If the E2E_NETWORK_NAME is set, use that for the network name.
	// Otherwise, return the default network name.
	if os.Getenv("E2E_NETWORK_NAME") != "" {
		return os.Getenv("E2E_NETWORK_NAME")
	}

	return defaultNetworkName
}

func buildConfigFromTemplate(tmpl string, data interface{}) string {
	t, err := template.New("config").Parse(tmpl)
	if err != nil {
		panic(err)
	}

	w := &strings.Builder{}
	if err = t.Execute(w, data); err != nil {
		panic(err)
	}

	return w.String()
}

var (
	networkName = getNetworkName()

	schemaConfig = `
schema_config:
  configs:
    - from: 2020-10-24
      store: boltdb-shipper
      object_store: filesystem
      schema: v11
      index:
        prefix: index_
        period: 24h
`

	defaultConfig = buildConfigFromTemplate(defaultConfigTemplate, struct {
		BucketName     string
		MinioAccessKey string
		MinioSecretKey string
		MinioEndpoint  string
		SchemaConfig   string
	}{
		BucketName:     bucketName,
		MinioAccessKey: e2edb.MinioAccessKey,
		MinioSecretKey: e2edb.MinioSecretKey,
		MinioEndpoint:  fmt.Sprintf("%s-minio-9001:9001", networkName),
		SchemaConfig:   schemaConfig,
	})

	defaultConfigTemplate = `
server:
  http_listen_port: 3100

ingester:
  lifecycler:
    address: 127.0.0.1
    ring:
      kvstore:
        store: inmemory
      replication_factor: 1
    final_sleep: 0s
    min_ready_duration: 0s
  chunk_idle_period: 10s      # Any chunk not receiving new logs in this time will be flushed
  max_chunk_age: 10s          # All chunks will be flushed when they hit this age, default is 1h
  chunk_target_size: 1048576  # Loki will attempt to build chunks up to 1.5MB, flushing first if chunk_idle_period or max_chunk_age is reached first
  chunk_retain_period: 30s    # Must be greater than index read cache TTL if using an index cache (Default index read cache TTL is 5m)
  max_transfer_retries: 0     # Chunk transfers disabled

{{.SchemaConfig}}

storage_config:
  boltdb_shipper:
    active_index_directory: /data/boltdb-shipper-active
    cache_location: /data/boltdb-shipper-cache
    cache_ttl: 24h         # Can be increased for faster performance over longer query periods, uses more disk space
    shared_store: filesystem
  aws:
    bucketnames:       {{.BucketName}}
    access_key_id:     {{.MinioAccessKey}}
    secret_access_key: {{.MinioSecretKey}}
    endpoint:          {{.MinioEndpoint}}
    insecure:          true

compactor:
  working_directory: /data/boltdb-shipper-compactor
  shared_store: filesystem

ruler:
  storage:
    type: local
    local:
      directory: /data/rules
  rule_path: /data/rules-temp
  alertmanager_url: http://localhost:9093
  ring:
    kvstore:
      store: inmemory
  enable_api: true
`
)
