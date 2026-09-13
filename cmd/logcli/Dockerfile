ARG GO_VERSION=1.27@sha256:f44f6e88636cfb311f9ebace870ded69d943f227bb3cb27d32ffd84ea18c43ea
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
