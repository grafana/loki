ARG GO_VERSION=1.24
FROM golang:${GO_VERSION} AS build

COPY . /src/loki
WORKDIR /src/loki
RUN make clean && CGO_ENABLED=0 go build ./cmd/logql-analyzer/

FROM gcr.io/distroless/static

COPY --from=build /src/loki/logql-analyzer /usr/bin/logql-analyzer

ENTRYPOINT [ "/usr/bin/logql-analyzer" ]
