# Running Loki

Currently there are four ways to try out Loki, in order from easier to hardest:
- [using our free hosted demo](#free-hosted-demo)
- [running it locally with Docker](#run-locally-with-docker)
- [building from source](#build-and-run-from-source)
- [using our Ksonnet config to run a fully-blown production setup](ksonnet/)

## Free Hosted Demo

Grafana is running a free, hosted demo cluster of Loki; instructions for getting access can be found at [grafana.com](https://grafana.com/loki).

## Run Locally Using Docker

The Docker images for [Loki](https://hub.docker.com/r/grafana/loki/) and [Promtail](https://hub.docker.com/r/grafana/promtail/) are available on DockerHub.

To test locally, we recommend using the docker-compose.yaml file in this directory:

1. `git clone` this repository locally (or just copy the contents of the docker-compose file locally into a file named `docker-compose.yaml`)
1. `cd loki/production`
1. If you have an older cached version of the `grafana/grafana:master` container then start by doing either:

   ```bash
   docker-compose pull
   ```

1. `docker-compose up`
1. Follow the [steps for configuring the datasource in Grafana](../docs/usage.md) and set the URL field to: `http://loki:3100`

## Build and Run From Source

Loki can be run in a single host, no-dependencies mode using the following commands.

You need `go` [v1.10+](https://golang.org/dl/) installed locally.

```bash
$ go build ./cmd/loki
$ ./loki -config.file=./docs/loki-local-config.yaml
...
```

To run Promtail, use the following commands:

```bash
$ go build ./cmd/promtail
$ ./promtail -config.file=./docs/promtail-local-config.yaml
...
```

Grafana is Loki's UI, so you'll also want to run one of those:

```bash
$ docker run -ti -p 3000:3000 -e "GF_EXPLORE_ENABLED=true" grafana/grafana:master
```

In the Grafana UI (http://localhost:3000), log in with "admin"/"admin", add a new "Grafana Loki" datasource for `http://host.docker.internal:3100`, then go to explore and enjoy!
