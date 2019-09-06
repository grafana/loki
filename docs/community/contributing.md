# Contributing to Loki

Loki uses [GitHub](https://github.com/grafana/loki) to manage reviews of pull requests:

- If you have a trivial fix or improvement, go ahead and create a pull request.
- If you plan to do something more involved, discuss your ideas on the relevant GitHub issue (creating one if it doesn't exist).

## Steps to contribute

To contribute to Loki, you must clone it into your `$GOPATH` and add your fork
as a remote.

```bash
$ git clone https://github.com/grafana/loki.git $GOPATH/src/github.com/grafana/loki
$ cd $GOPATH/src/github.com/grafana/loki
$ git rmeote add fork <FORK_URL>

# Make some changes!

$ git add .
$ git commit -m "docs: fix spelling error"
$ git push -u fork HEAD

# Open a PR!
```

Note that if you downloaded Loki using `go get`, the message `package github.com/grafana/loki: no Go files in /go/src/github.com/grafana/loki`
is normal and requires no actions to resolve.

## Contribute to the Helm Chart

Please follow the [Helm documentation](../../production/helm/README.md).
