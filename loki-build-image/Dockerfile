ARG GO_VERSION=latest
ARG BUILD_IMAGE=golang:$GO_VERSION

FROM $BUILD_IMAGE

ARG SRC_DIR=/src/loki
ARG INSTALL_WORKFLOW_DEPS_ARGS

COPY .github $SRC_DIR/.github

RUN git config --global --add safe.directory $SRC_DIR
RUN echo "Install dependencies ..."
RUN $SRC_DIR/.github/vendor/github.com/grafana/loki-release/workflows/install_workflow_dependencies.sh $INSTALL_WORKFLOW_DEPS_ARGS

WORKDIR $SRC_DIR
ENTRYPOINT [ "/usr/bin/make" ]
