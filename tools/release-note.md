This is release `${CIRCLE_TAG}` of Loki.

### Notable changes:
:warning: **ADD RELEASE NOTES HERE** :warning:

#### Binary
We provide pre-compiled binary executables for the most common operating systems and architectures.
Pick one from the list below and download it:

```bash
$ wget $YOUR_BINARY
$ unzip $YOUR_BINARY
$ chmod +x $YOUR_BINARY
$ mv $YOUR_BINARY /usr/local/bin/$YOUR_BINARY
```

### Installation:
The components of Loki are currently distributed in plain binary form and as Docker container images. Choose what fits your use-case best.

#### Docker container:
* https://hub.docker.com/r/grafana/loki
* https://hub.docker.com/r/grafana/promtail
```bash
$ docker pull "grafana/loki:${CIRCLE_TAG}"
$ docker pull "grafana/promtail:${CIRCLE_TAG}"
```
