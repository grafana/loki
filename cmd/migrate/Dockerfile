<<<<<<< HEAD
ARG GO_VERSION=1.22
FROM golang:${GO_VERSION} as build
=======
ARG GO_VERSION=1.23
FROM golang:${GO_VERSION} AS build
>>>>>>> 5f670a4b1 (chore(deps): Move from base-nossl to static (#15203))
COPY . /src/loki
WORKDIR /src/loki
RUN make clean && make BUILD_IN_CONTAINER=false migrate

FROM gcr.io/distroless/static:debug

COPY --from=build /src/loki/cmd/migrate/migrate /usr/bin/migrate
SHELL [ "/busybox/sh", "-c" ]
RUN ln -s /busybox/sh /bin/sh
ENTRYPOINT [ "/busybox/tail", "-f", "/dev/null" ]
