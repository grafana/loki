ARG GO_VERSION=latest
ARG BUILD_IMAGE=golang:$GO_VERSION

FROM $BUILD_IMAGE

ARG SRC_DIR=/src/loki
ARG INSTALL_WORKFLOW_DEPS_ARGS

COPY .github $SRC_DIR/.github

RUN git config --global --add safe.directory $SRC_DIR
RUN echo "Install dependencies ..."
RUN $SRC_DIR/.github/vendor/github.com/grafana/loki-release/workflows/install_workflow_dependencies.sh $INSTALL_WORKFLOW_DEPS_ARGS

# govulncheck is used by .github/workflows/govulncheck.yml to scan PRs for
# reachable known-CVE vulnerabilities. Pre-installed here so the workflow can
# run as a thin container job without paying the `go install` cost on every PR.
# Pinned for reproducibility — bump in lockstep with security tooling refresh.
RUN GO111MODULE=on go install golang.org/x/vuln/cmd/govulncheck@v1.3.0

WORKDIR $SRC_DIR
ENTRYPOINT [ "/usr/bin/make" ]
