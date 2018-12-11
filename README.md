<p align="center"><img src="docs/logo_and_name.png" width="600" alt="Loki Logo"></p>

<p align="center">
<a href="https://circleci.com/gh/grafana/loki/tree/master"><img src="https://circleci.com/gh/grafana/loki.svg?style=svg&circle-token=618193e5787b2951c1ea3352ad5f254f4f52313d" alt="CircleCI" /></a>
<a href="http://slack.raintank.io/"><img src="https://img.shields.io/badge/join%20slack-%23loki-brightgreen.svg" alt="Slack" /></a>
</p>

# Loki: like Prometheus, but for logs.

Loki is a horizontally-scalable, highly-available, multi-tenant log aggregation system inspired by Prometheus.  It is designed to be very cost effective and easy to operate, as it does not index the contents of the logs, but rather a set of labels for each log stream.

Compared to other log aggregation systems, Loki:

- does not do full text indexing on logs. By storing compressed, unstructured logs and only indexing metadata, Loki is simpler to operate and cheaper to run.
- indexes and groups log streams using the same labels you’re already using with Prometheus, enabling you to seamlessly switch between metrics and logs using the same labels that you’re already using with Prometheus.
- is an especially good fit for storing Kubernetes Pod logs; metadata such as Pod labels is automatically scraped and indexed.
- has native support in Grafana (already in the nightly builds, will be included in Grafana 6.0).

Loki consists of 3 components:

- `loki` is the main server, responsible for storing logs and processing queries.
- `promtail` is the agent, responsible for gathering logs and sending them to loki.
- [Grafana](https://github.com/grafana/grafana) for the UI.

## Getting started

Currently there are three ways to try out Loki: using our free hosted demo, running it locally with Docker or building from source.

### Free Hosted Demo

Grafana is running a free, hosted demo cluster of Loki; instructions for getting access can be found at [grafana.com](https://grafana.com/loki).

### Run Locally Using Docker

The Docker images for [Loki](https://hub.docker.com/r/grafana/loki/) and [Promtail](https://hub.docker.com/r/grafana/promtail/) are available on DockerHub.

To test locally using `docker run`:

1. Create a Docker network that the Docker containers can share:
    ```bash
    docker network create loki
    ```

2. Start the Loki server:
    ```bash
    docker run --name loki --network=loki -p 3100:3100 --volume "$PWD/docs:/etc/loki" grafana/loki:master -config.file=/etc/loki/loki-local-config.yaml
    ```

3. Then start the Promtail agent. The default config polls the contents of your `/var/log` directory.
    ```bash
    docker run --name promtail --network=loki --volume "$PWD/docs:/etc/promtail" --volume "/var/log:/var/log" --network="container:loki" grafana/promtail:master -config.file=/etc/promtail/promtail-local-config.yaml
    ```

4. If you also want to run Grafana in docker:
    ```bash
    docker run --name grafana --network=loki -p 3000:3000 -e "GF_EXPLORE_ENABLED=true" grafana/grafana:master
    ```

5. Follow the steps for configuring the datasource in Grafana in the section below and set the URL field to: `http://loki:3100`

Another option is to use the docker-compose file in the docs directory:

1. git clone this repo locally (or just copy the contents of the docker-compose file locally into a file named `docker-compose.yaml`)
2. `cd loki/docs`
3. `docker-compose up`

If you have have an older cached version of the grafana/grafana:master container then start by doing either:

```bash
docker pull grafana/grafana:master
```

Or for docker-compose:

```bash
docker-compose pull
```

### Configuring the Loki Datasource in Grafana

Grafana ships with built-in support for Loki in the [latest nightly builds](https://grafana.com/grafana/download). Loki support will be officially released in Grafana 6.0.

1. Open the side menu by clicking the Grafana icon in the top header.
2. In the side menu under the Dashboards link you should find a link named Data Sources.
3. Click the `+ Add data source` button in the top header.
4. Choose Loki from the list.
5. The http URL field should be the address of your Loki server e.g. `http://localhost:3100`

Read more about the Explore feature in the [Grafana docs](http://docs.grafana.org/features/explore) and on how to search and filter logs with Loki.

### Build and Run Loki Locally

Loki can be run in a single host, no-dependencies mode using the following commands.

```bash
$ go build ./cmd/loki
$ ./loki -config.file=./docs/loki-local-config.yaml
...
```

To run promtail, use the following commands:

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

## Grafana Provisioning

It is possible to configure Grafana datasources using config files with Grafana’s provisioning system. You can read more about how it works in the [Grafana documentation](http://docs.grafana.org/administration/provisioning/#datasources).

Here is a simple example of the provisioning yaml config for the Grafana Loki datasource:

```yaml
apiVersion: 1

datasources:
  - name: Loki
    type: loki
    access: proxy
    url: http://localhost:3100
    editable: false
```

Example with basic auth:

```yaml
apiVersion: 1

datasources:
  - name: Loki
    type: loki
    access: proxy
    url: http://localhost:3100
    editable: false
    basicAuth: true
    basicAuthUser: my_user
    basicAuthPassword: test_password
```

## Further Reading

- The original [design doc](https://docs.google.com/document/d/11tjK_lvp1-SVsFZjgOTr1vV3-q6vBAsZYIQ5ZeYBkyM/view) for Loki is a good source for discussion of the motivation and design decisions.
- David Kaltschmidt KubeCon 2018 talk "[On the OSS Path to Full Observability with Grafana](https://kccna18.sched.com/event/GrXC/on-the-oss-path-to-full-observability-with-grafana-david-kaltschmidt-grafana-labs)"
