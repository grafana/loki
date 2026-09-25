ARG GO_VERSION=1.27@sha256:3680233e3204827fbdc66088528ae6d4b3d034f51d03a99d454f6de034888244
FROM golang:${GO_VERSION} AS build
COPY . /src/loki
WORKDIR /src/loki
RUN make clean && make BUILD_IN_CONTAINER=false migrate

FROM gcr.io/distroless/static:debug

COPY --from=build /src/loki/cmd/migrate/migrate /usr/bin/migrate
SHELL [ "/busybox/sh", "-c" ]
RUN ln -s /busybox/sh /bin/sh
ENTRYPOINT [ "/busybox/tail", "-f", "/dev/null" ]
