## Installation

Loki is provided as pre-compiled binaries, or as a Docker container image.

### Docker container (Recommended)
If you want to run in a container, use our Docker image:
```bash
$ docker pull "grafana/loki:v0.2.0"
```

### Binary
If you want to use plain binaries instead, head over to the
[Releases](https://github.com/grafana/loki/releases) on GitHub and download the
most recent one for you operating system and architecture.

Example (Linux, `amd64`), Loki `v0.2.0`:
```bash
# download binary (adapt app, os and arch as needed)
$ curl -fSL -o "/usr/local/bin/loki.gz" "https://github.com/grafana/loki/releases/download/v0.2.0/loki-linux-amd64.gz"
$ gunzip "/usr/local/bin/loki.gz"

# make sure it is executable
$ chmod a+x "/usr/local/bin/loki"
```


## Running

After you have Loki installed, you need to pick between one of the two operation modes:

You can either run all three components (Distributor, Ingester and Querier)
together as a single fat process, which allows easy operations because you do
not need to bother about inter-service communication. This is usually
recommended for most use-cases.  
This still allows to scale horizontally, but keep in mind you scale all three
services at once.

The other option is the distributed mode, where each component runs on its own
and communicates with the others using gRPC.  
This especially allows fine-grained control over scaling, because you can scale
the services individually.

### Single Process

#### `docker-compose`
To try it out locally, or to run on only a few systems, `docker-compose` is a
good choice.

Check out the [`docker-compose.yml` file on
GitHub](https://github.com/grafana/loki/blob/master/production/docker-compose.yaml).

#### `helm`
If want to quickly get up and running on Kubernetes, `helm` got you covered:

```bash
# add the loki repository to helm
$ helm repo add loki https://grafana.github.io/loki/charts
$ helm update
```

You can then choose between deploying the whole stack (Loki and Promtail), or
each component individually:

```bash
# whole stack
$ helm upgrade --install loki loki/loki-stack

# only Loki
$ helm upgrade --install loki loki/loki

# only Promtail
helm upgrade --install promtail loki/promtail --set "loki.serviceName=loki"
```

Refer to the
[Chart](https://github.com/grafana/loki/tree/master/production/helm) for more
information

### Distributed
Running in distributed mode is currently pretty hard, because of the multitude
of challenges that one encounters.

We do this internally, but cannot really recommend anyone to try as well.  
If you really want to, you can take a look at our [production
setup](https://github.com/grafana/loki/tree/master/production/ksonnet). You have
been warned :P
