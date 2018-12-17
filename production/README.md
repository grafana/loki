# Running Loki

Currently there are five ways to try out Loki, in order from easier to hardest:

- [Using our free hosted demo](#free-hosted-demo)
- [Running it locally with Docker](#run-locally-using-docker)
- [Using Helm to deploy on Kubernetes](helm/)
- [Building from source](#build-and-run-from-source)
- [Using our Ksonnet config to run a fully-blown production setup](ksonnet/)

## Free Hosted Demo

Grafana is running a free, hosted demo cluster of Loki; instructions for getting access can be found at [grafana.com](https://grafana.com/loki).

## Run Locally Using Docker

The Docker images for [Loki](https://hub.docker.com/r/grafana/loki/) and [Promtail](https://hub.docker.com/r/grafana/promtail/) are available on DockerHub.

To test locally, we recommend using the docker-compose.yaml file in this directory:

1. Either `git clone` this repository locally and `cd loki/production`, or download a copy of the [docker-compose.yaml](docker-compose.yaml) locally.

1. Ensure you have the freshest, most up to date container images:

   ```bash
   docker-compose pull
   ```

1. Run the stack on your local docker:

   ```bash
   docker-compose up
   ```

1. Grafana should now be available at http://localhost:3000/.  Follow the [steps for configuring the datasource in Grafana](../docs/usage.md) and set the URL field to `http://loki:3100`.

For instructions on how to use loki, see [our usage docs](../docs/usage.md).

## Build and Run From Source

Loki can be run in a single host, no-dependencies mode using the following commands.

You need `go` [v1.10+](https://golang.org/dl/) installed locally.

```bash
$ go build ./cmd/loki
$ ./loki -config.file=./cmd/loki/loki-local-config.yaml
...
```

To run Promtail, use the following commands:

```bash
$ go build ./cmd/promtail
$ ./promtail -config.file=./cmd/promtail/promtail-local-config.yaml
...
```

Grafana is Loki's UI, so you'll also want to run one of those:

```bash
$ docker run -ti -p 3000:3000 -e "GF_EXPLORE_ENABLED=true" grafana/grafana:master
```

Grafana should now be available at http://localhost:3000/.  Follow the [steps for configuring the datasource in Grafana](../docs/usage.md) and set the URL field to `http://host.docker.internal:3100`.

For instructions on how to use loki, see [our usage docs](../docs/usage.md).
