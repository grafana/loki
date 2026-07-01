ARG GO_VERSION=latest
ARG BUILD_IMAGE=golang:$GO_VERSION

FROM $BUILD_IMAGE

ARG SRC_DIR=/src/loki
ARG INSTALL_WORKFLOW_DEPS_ARGS
ARG USER=loki

COPY .github $SRC_DIR/.github

RUN git config --global --add safe.directory $SRC_DIR
RUN echo "Install dependencies ..."
RUN $SRC_DIR/.github/vendor/github.com/grafana/loki-release/workflows/install_workflow_dependencies.sh $INSTALL_WORKFLOW_DEPS_ARGS

# wiresmith is the protobuf compiler used by the `make wiresmith-protos` target.
# Installed directly here (not via the loki-release-vendored deps script) so the
# binary is present on PATH without editing vendored files.
RUN go install github.com/grafana/wiresmith/cmd/wiresmith@v0.0.0-20260618160438-f15959a1e4e7

RUN useradd -ms /bin/bash $USER
USER $USER

WORKDIR $SRC_DIR
ENTRYPOINT [ "/usr/bin/make" ]
