# Container-based end-to-end tests

These tests start Loki **containers** on a Docker network and exercise them
via their API. They use the [`github.com/grafana/e2e`](https://github.com/grafana/e2e) framework
(`NewScenario`, `NewHTTPService`, readiness probes, …), which shells out to the
`docker` CLI to manage containers.

This is different from the tests under [`integration`](../../integration),
which run Loki targets inside the test binary. Here we test the actual published
container image end to end.

## Requirements

- A running Docker daemon.
- A Loki image to run. By default the tests read the image from the `LOKI_IMAGE`
  environment variable, falling back to `grafana/loki:latest` when unset.

## Running

The easiest way is the Makefile target, which builds the local image and points
the tests at it:

```sh
make test-integration-e2e
```

Or run them directly against an image of your choice:

```sh
# build the local image first (produces grafana/loki:<image-tag>)
make loki-image

LOKI_IMAGE=grafana/loki:$(./tools/image-tag) \
  go test -tags requires_docker -v -timeout 15m ./integration/e2e/...
```

The tests are guarded by the `requires_docker` build tag, so a normal
`go test ./...` does not pick them up.

## Adding a test

```go
s, err := e2e.NewScenario("loki-e2e")
require.NoError(t, err)
defer s.Close()

require.NoError(t, os.WriteFile(filepath.Join(s.SharedDir(), "config.yaml"), []byte(singleBinaryConfig), 0o644))

loki := NewSingleBinary("loki")
require.NoError(t, s.StartAndWaitReady(loki))

c := NewClient(loki.HTTPEndpoint(), "")
// ... push and query
```

The scenario's shared directory is mounted into every container at `/shared`,
which is where the config and the filesystem object store live.
