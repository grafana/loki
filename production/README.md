# Running Loki

Currently there are five ways to try out Loki, in order from easier to hardest:

- [Using our free hosted demo](#free-hosted-demo)
- [Running it locally with Docker](#run-locally-using-docker)
- [Using Helm to deploy on Kubernetes](#using-helm-to-deploy-on-kubernetes)
- [Building from source](#build-and-run-from-source)
- [Get inspired by our production setup](#get-inspired-by-our-production-setup)

For the various ways to run `promtail`, the tailing agent, see our [Promtail documentation](../docs/promtail-setup.md).

## Get a Free Hosted Demo of Grafana Cloud: Logs

Grafana is running a free, hosted demo cluster of Loki; instructions for getting access can be found at [grafana.com/loki](https://grafana.com/loki).

In addition, the demo also includes an allotment of complimentary metrics (Prometheus or Graphite) to help illustrate the experience of easily switching between logs and metrics.

## Run Locally Using Docker

The Docker images for [Loki](https://hub.docker.com/r/grafana/loki/) and [Promtail](https://hub.docker.com/r/grafana/promtail/) are available on DockerHub.

To test locally, we recommend using the `docker-compose.yaml` file in this directory.
It will start containers for promtail, Loki, and Grafana.

1. Either `git clone` this repository locally and `cd loki/production`, or download a copy of the [docker-compose.yaml](docker-compose.yaml) locally.

1. Ensure you have the freshest, most up to date container images:

   ```bash
   docker-compose pull
   ```

1. Run the stack on your local docker:

   ```bash
   docker-compose up
   ```

1. Grafana should now be available at http://localhost:3000/. Log in with `admin` / `admin` and follow the [steps for configuring the datasource in Grafana](../docs/usage.md), using `http://loki:3100` for the URL field.

_Note_: When running locally, promtail starts before loki is ready. This can lead to the error message "Data source connected, but no labels received." After a couple seconds, Promtail will forward all newly created log messages correctly.
Until this is fixed we recommend [building and running from source](#build-and-run-from-source).

For instructions on how to query Loki, see [our usage docs](../docs/usage.md).

## Using Helm to deploy on Kubernetes

There is a [Helm chart](helm) to deploy Loki and promtail to Kubernetes.

## Build and Run From Source

Loki can be run in a single host, no-dependencies mode using the following commands.

You need `go` [v1.10+](https://golang.org/dl/) installed locally.

```bash

$ go get github.com/grafana/loki
$ cd $GOPATH/src/github.com/grafana/loki # GOPATH is $HOME/go by default.

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

Grafana is Loki's UI. To query your logs you need to start Grafana as well:

```bash
$ docker run -ti -p 3000:3000 grafana/grafana:master
```

Grafana should now be available at http://localhost:3000/. Follow the [steps for configuring the datasource in Grafana](../docs/usage.md) and set the URL field to `http://host.docker.internal:3100`.

For instructions on how to use loki, see [our usage docs](../docs/usage.md).

## Get inspired by our production setup

We run Loki on Kubernetes with the help of ksonnet.
You can take a look at [our production setup](ksonnet/).

To learn more about ksonnet, check out its [documentation](https://ksonnet.io).
