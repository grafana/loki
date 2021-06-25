# Grafana Loki

This sets up Grafana Loki through [docker-compose](https://docs.docker.com/compose/). It also runs Grafana and Promtail.

## Usage

### Spin up

Build it first:

```console
$ docker-compose build
```

Then run it:

```console
$ docker-compose up -d
```

Go to http://localhost:3000 to access Grafana.