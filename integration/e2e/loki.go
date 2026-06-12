//go:build requires_docker

package e2e

import (
	"os"

	"github.com/grafana/e2e"
)

const (
	httpPort = 3100
	grpcPort = 9095
)

// singleBinaryConfig is a minimal all-in-one Loki configuration using the local
// filesystem for object storage and an in-memory ring.
// Storage lives under /loki, an ephemeral in-container path that is discarded
// when the container is removed (--rm).
const singleBinaryConfig = `
auth_enabled: false

server:
  http_listen_port: 3100
  grpc_listen_port: 9095
  log_level: warn

common:
  instance_addr: 127.0.0.1
  path_prefix: /loki
  storage:
    filesystem:
      chunks_directory: /loki/chunks
      rules_directory: /loki/rules
  replication_factor: 1
  ring:
    kvstore:
      store: inmemory

schema_config:
  configs:
    - from: 2020-10-24
      store: tsdb
      object_store: filesystem
      schema: v13
      index:
        prefix: index_
        period: 24h
`

// NewSingleBinary returns an all-in-one Loki service (every target in one
// process) reading the config written to /shared/config.yaml. Extra command
// line flags can be appended to override config values.
func NewSingleBinary(name string, flags ...string) *e2e.HTTPService {
	args := append([]string{
		"-config.file=/shared/config.yaml",
		"-target=all",
	}, flags...)

	img := "grafana/loki:latest"
	if e := os.Getenv("LOKI_IMAGE"); e != "" {
		img = e
	}

	svc := e2e.NewHTTPService(
		name,
		img,
		// The image's entrypoint is the loki binary, so disable it and invoke
		// the binary explicitly with our arguments.
		e2e.NewCommandWithoutEntrypoint("/usr/bin/loki", args...),
		e2e.NewHTTPReadinessProbe(httpPort, "/ready", 200, 299),
		httpPort,
		grpcPort,
	)

	return svc
}
