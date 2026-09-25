ARG GO_VERSION=1.27@sha256:3680233e3204827fbdc66088528ae6d4b3d034f51d03a99d454f6de034888244
ARG IMAGE_TAG
FROM golang:${GO_VERSION} AS build

COPY . /src/loki
WORKDIR /src/loki
RUN make clean && make BUILD_IN_CONTAINER=false IMAGE_TAG=${IMAGE_TAG} logcli


FROM gcr.io/distroless/static:debug

COPY --from=build /src/loki/cmd/logcli/logcli /usr/bin/logcli
SHELL [ "/busybox/sh", "-c" ]
RUN ln -s /busybox/sh /bin/sh

ENTRYPOINT [ "/usr/bin/logcli" ]
