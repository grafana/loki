ARG GO_VERSION=1.27@sha256:e519d3b0a5b285e5aa53ab9eef02bc6bbb9cb04608389b743a136a9ce75f0d1e
FROM golang:${GO_VERSION} AS build
COPY . /src/loki
WORKDIR /src/loki
RUN make clean && make BUILD_IN_CONTAINER=false migrate

FROM gcr.io/distroless/static:debug

COPY --from=build /src/loki/cmd/migrate/migrate /usr/bin/migrate
SHELL [ "/busybox/sh", "-c" ]
RUN ln -s /busybox/sh /bin/sh
ENTRYPOINT [ "/busybox/tail", "-f", "/dev/null" ]
