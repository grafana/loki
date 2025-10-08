ARG GO_VERSION=1.24
FROM golang:${GO_VERSION} AS build
COPY . /src/loki
WORKDIR /src/loki
RUN make clean && make BUILD_IN_CONTAINER=false migrate

FROM gcr.io/distroless/static

COPY --from=build /src/loki/cmd/migrate/migrate /usr/bin/migrate
ENTRYPOINT [ "/usr/bin/migrate" ]
