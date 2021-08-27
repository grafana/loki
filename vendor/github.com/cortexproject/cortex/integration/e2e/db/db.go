package e2edb

import (
	"fmt"
	"strings"

	"github.com/cortexproject/cortex/integration/e2e"
	"github.com/cortexproject/cortex/integration/e2e/images"
)

const (
	MinioAccessKey = "Cheescake"
	MinioSecretKey = "supersecret"
)

// NewMinio returns minio server, used as a local replacement for S3.
func NewMinio(port int, bktNames ...string) *e2e.HTTPService {
	minioKESGithubContent := "https://raw.githubusercontent.com/minio/kes/master"
	commands := []string{
		fmt.Sprintf("curl -sSL --tlsv1.2 -O '%s/root.key' -O '%s/root.cert'", minioKESGithubContent, minioKESGithubContent),
	}

	for _, bkt := range bktNames {
		commands = append(commands, fmt.Sprintf("mkdir -p /data/%s", bkt))
	}
	commands = append(commands, fmt.Sprintf("minio server --address :%v --quiet /data", port))

	m := e2e.NewHTTPService(
		fmt.Sprintf("minio-%v", port),
		images.Minio,
		// Create the "cortex" bucket before starting minio
		e2e.NewCommandWithoutEntrypoint("sh", "-c", strings.Join(commands, " && ")),
		e2e.NewHTTPReadinessProbe(port, "/minio/health/ready", 200, 200),
		port,
	)
	m.SetEnvVars(map[string]string{
		"MINIO_ACCESS_KEY": MinioAccessKey,
		"MINIO_SECRET_KEY": MinioSecretKey,
		"MINIO_BROWSER":    "off",
		"ENABLE_HTTPS":     "0",
		// https://docs.min.io/docs/minio-kms-quickstart-guide.html
		"MINIO_KMS_KES_ENDPOINT":  "https://play.min.io:7373",
		"MINIO_KMS_KES_KEY_FILE":  "root.key",
		"MINIO_KMS_KES_CERT_FILE": "root.cert",
		"MINIO_KMS_KES_KEY_NAME":  "my-minio-key",
	})
	return m
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
