This is release `${CIRCLE_TAG}` of Loki.

### Notable changes:
:warning: **ADD RELEASE NOTES HERE** :warning:

### Installation:
The components of Loki are currently distributed in plain binary form and as Docker container images. Choose what fits your use-case best.

#### Binary:
```bash
# download a binary (adapt app, os and arch as needed)
$ curl -fSL -o "/usr/local/bin/loki.zip" "https://github.com/grafana/loki/releases/download/${CIRCLE_TAG}/loki-linux-amd64.zip"
$ unzip "/usr/local/bin/loki.gz"

# make sure it is executable
$ chmod a+x "/usr/local/bin/loki"
```

#### Docker container:
* https://hub.docker.com/r/grafana/loki
* https://hub.docker.com/r/grafana/promtail
```bash
$ docker pull "grafana/loki:${CIRCLE_TAG}"
$ docker pull "grafana/promtail:${CIRCLE_TAG}"
```
