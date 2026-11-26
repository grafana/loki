// Package e2edb provides pre-built database and storage service definitions for e2e tests.
package e2edb

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/grafana/e2e"
	"github.com/grafana/e2e/images"
)

const (
	MinioAccessKey = "Cheescake"
	MinioSecretKey = "supersecret"
)

// NewMinio returns minio server, used as a local replacement for S3.
func NewMinio(port int, bktNames ...string) *e2e.HTTPService {
	return newMinio(port, map[string]string{}, bktNames...)
}

// NewMinioWithKES returns minio server, configured to talk to a KES service.
func NewMinioWithKES(port int, kesEndpoint, rootKeyFile, rootCertFile, caCertFile string, bktNames ...string) *e2e.HTTPService {
	kesEnvVars := map[string]string{
		"MINIO_KMS_KES_ENDPOINT":  kesEndpoint,
		"MINIO_KMS_KES_KEY_FILE":  filepath.Join(e2e.ContainerSharedDir, rootKeyFile),
		"MINIO_KMS_KES_CERT_FILE": filepath.Join(e2e.ContainerSharedDir, rootCertFile),
		"MINIO_KMS_KES_CAPATH":    filepath.Join(e2e.ContainerSharedDir, caCertFile),
		"MINIO_KMS_KES_KEY_NAME":  "my-minio-key",
	}
	return newMinio(port, kesEnvVars, bktNames...)
}

func newMinio(port int, envVars map[string]string, bktNames ...string) *e2e.HTTPService {
	commands := []string{}
	for _, bkt := range bktNames {
		commands = append(commands, fmt.Sprintf("mkdir -p /data/%s", bkt))
	}
	commands = append(commands, fmt.Sprintf("silo server --address :%v --quiet /data", port))

	m := e2e.NewHTTPService(
		fmt.Sprintf("minio-%v", port),
		images.Minio,
		// Create the buckets before starting minio
		e2e.NewCommandWithoutEntrypoint("sh", "-c", strings.Join(commands, " && ")),
		e2e.NewHTTPReadinessProbe(port, "/minio/health/ready", 200, 200),
		port,
	)
	envVars["MINIO_ACCESS_KEY"] = MinioAccessKey
	envVars["MINIO_SECRET_KEY"] = MinioSecretKey
	envVars["MINIO_BROWSER"] = "off"
	envVars["ENABLE_HTTPS"] = "0"
	m.SetEnvVars(envVars)
	return m
}

// NewRustFS returns a RustFS server, an S3-compatible alternative to NewMinio. It keeps
// the minio-<port> hostname and the MinioAccessKey/MinioSecretKey credentials so callers
// can switch without changing their S3 configuration.
func NewRustFS(port int, bktNames ...string) *e2e.HTTPService {
	commands := []string{}
	for _, bkt := range bktNames {
		commands = append(commands, fmt.Sprintf("mkdir -p /data/%s", bkt))
	}
	commands = append(commands, fmt.Sprintf("rustfs --address :%v /data", port))

	m := e2e.NewHTTPService(
		fmt.Sprintf("minio-%v", port),
		images.RustFS,
		// Create the buckets before starting RustFS
		e2e.NewCommandWithoutEntrypoint("sh", "-c", strings.Join(commands, " && ")),
		e2e.NewHTTPReadinessProbe(port, "/minio/health/ready", 200, 200),
		port,
	)
	m.SetEnvVars(map[string]string{
		"RUSTFS_ACCESS_KEY":     MinioAccessKey,
		"RUSTFS_SECRET_KEY":     MinioSecretKey,
		"RUSTFS_CONSOLE_ENABLE": "false",
	})
	return m
}

// NewKES returns KES server, used as a local key management store
func NewKES(port int, serverName, serverKeyFile, serverCertFile, clientKeyFile, clientCertFile, rootCertFile, hostSharedDir string) (*e2e.HTTPService, error) {
	// Run this as a shell command, so sub-shell can evaluate 'identity' of root user.
	command := fmt.Sprintf("/kes server --addr 0.0.0.0:%d --key=%s --cert=%s --root=$(/kes tool identity of %s) --auth=off",
		port, filepath.Join(e2e.ContainerSharedDir, serverKeyFile), filepath.Join(e2e.ContainerSharedDir, serverCertFile), filepath.Join(e2e.ContainerSharedDir, clientCertFile))

	readinessProbe, err := e2e.NewHTTPSReadinessProbe(port, "/v1/status", serverName, filepath.Join(hostSharedDir, clientKeyFile), filepath.Join(hostSharedDir, clientCertFile), filepath.Join(hostSharedDir, rootCertFile), 200, 200)
	if err != nil {
		return nil, err
	}

	return e2e.NewHTTPService(
		"kes",
		images.KES,
		e2e.NewCommandWithoutEntrypoint("sh", "-c", command),
		readinessProbe,
		port,
	), nil
}

func NewConsul() *e2e.HTTPService {
	return e2e.NewHTTPService(
		"consul",
		images.Consul,
		// Run consul in "dev" mode so that the initial leader election is immediate
		e2e.NewCommand("agent", "-server", "-client=0.0.0.0", "-dev", "-log-level=err"),
		e2e.NewHTTPReadinessProbe(8500, "/v1/operator/autopilot/health", 200, 200, `"Healthy": true`),
		8500,
	)
}

func NewETCD() *e2e.HTTPService {
	return e2e.NewHTTPService(
		"etcd",
		images.ETCD,
		e2e.NewCommand("/usr/local/bin/etcd", "--listen-client-urls=http://0.0.0.0:2379", "--advertise-client-urls=http://0.0.0.0:2379", "--listen-metrics-urls=http://0.0.0.0:9000", "--log-level=error"),
		e2e.NewHTTPReadinessProbe(9000, "/health", 200, 204),
		2379,
		9000, // Metrics
	)
}

func NewDynamoDB() *e2e.HTTPService {
	return e2e.NewHTTPService(
		"dynamodb",
		images.DynamoDB,
		e2e.NewCommand("-jar", "DynamoDBLocal.jar", "-inMemory", "-sharedDb"),
		// DynamoDB doesn't have a readiness probe, so we check if the / works even if returns 400
		e2e.NewHTTPReadinessProbe(8000, "/", 400, 400),
		8000,
	)
}
