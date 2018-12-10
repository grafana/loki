# Loki: Like Prometheus, but for logs.

[![CircleCI](https://circleci.com/gh/grafana/loki/tree/master.svg?style=svg&circle-token=618193e5787b2951c1ea3352ad5f254f4f52313d)](https://circleci.com/gh/grafana/loki/tree/master) [Design doc](https://docs.google.com/document/d/11tjK_lvp1-SVsFZjgOTr1vV3-q6vBAsZYIQ5ZeYBkyM/edit)

Loki is a horizontally-scalable, highly-available, multi-tenant, log aggregation
system inspired by Prometheus.  It is designed to be very cost effective, as it does
not index the contents of the logs, but rather a set of labels for each log stream.

## Run it locally

Loki can be run in a single host, no-dependencies mode using the following commands.

Loki consists of 3 components; `loki` is the main server, responsible for storing
logs and processing queries.  `promtail` is the agent, responsible for gather logs
and sending them to loki and `grafana` as the UI.

To run loki, use the following commands:

```
$ go build ./cmd/loki
$ ./loki -config.file=./docs/loki-local-config.yaml
...
```

To run promtail, use the following commands:

```
$ go build ./cmd/promtail
$ ./promtail -config.file=./docs/promtail-local-config.yaml
...
```

Grafana is Loki's UI, so you'll also want to run one of those:

```
$ docker run -ti -p 3000:3000 -e "GF_EXPLORE_ENABLED=true" grafana/grafana:master
```

In the Grafana UI (http://localhost:3000), log in with "admin"/"admin", add a new "Grafana Logging" datasource for `http://host.docker.internal:3100`, then go to explore and enjoy!