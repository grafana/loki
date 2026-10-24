ARG GO_VERSION=1.27.3@sha256:f20d83a83e18dbd2a4714b8ffd4b30b287b6243962dd75608e86abcc63ba4efc
FROM golang:${GO_VERSION} AS build

COPY . /src/loki
WORKDIR /src/loki
RUN make clean && CGO_ENABLED=0 go build ./cmd/logql-analyzer/

FROM gcr.io/distroless/static:debug

COPY --from=build /src/loki/logql-analyzer /usr/bin/logql-analyzer
SHELL [ "/busybox/sh", "-c" ]
RUN ln -s /busybox/sh /bin/sh

ENTRYPOINT [ "/usr/bin/logql-analyzer" ]
