This is release `${DRONE_TAG}` of Loki.

### Notable changes:
:warning: **ADD RELEASE NOTES HERE** :warning:


### Installation:
The components of Loki are currently distributed in plain binary form and as Docker container images. Choose what fits your use-case best.

#### Docker container:
* https://hub.docker.com/r/grafana/loki
* https://hub.docker.com/r/grafana/promtail
```bash
$ docker pull "grafana/loki:${DRONE_TAG_NO_V}"
$ docker pull "grafana/promtail:${DRONE_TAG_NO_V}"
```

#### Binary
We provide pre-compiled binary executables for the most common operating systems and architectures.
Choose from the assets below for the application and architecture matching your system.
Example for `Loki` on the `linux` operating system and `amd64` architecture:

```bash
$ curl -O -L "https://github.com/grafana/loki/releases/download/${DRONE_TAG}/loki-linux-amd64.zip"
# extract the binary
$ unzip "loki-linux-amd64.zip"
# make sure it is executable
$ chmod a+x "loki-linux-amd64"
```
