This is release `${CIRCLE_TAG}` of Loki. 

### Notable changes:
:warning: **ADD RELEASE NOTES HERE** :warning:

### Installation:
The components of Loki are currently distributed in plain binary form and as Docker container images. Choose what fits your use-case best.

#### Docker container:
* https://hub.docker.com/r/grafana/loki
* https://hub.docker.com/r/grafana/promtail
```bash
$ docker pull "grafana/loki:${CIRCLE_TAG}"
$ docker pull "grafana/promtail:${CIRCLE_TAG}"
```

#### Binary:

Choose from the assets below for the application and architecture matching your system.

Example for `Loki` on the `linux` operating system and `amd64` architecture:

```bash
# curl -L "https://github.com/grafana/loki/releases/download/${CIRCLE_TAG}/loki-linux-amd64.gz

# extract the binary
$ gunzip "loki-linux-amd64.gz"

# make sure it is executable
$ chmod a+x "loki-linux-amd64"
```

