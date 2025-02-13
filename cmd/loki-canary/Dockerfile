ARG GO_VERSION=1.23
FROM golang:${GO_VERSION} AS build
ARG IMAGE_TAG

COPY . /src/loki
WORKDIR /src/loki
RUN make clean && make BUILD_IN_CONTAINER=false IMAGE_TAG=${IMAGE_TAG} loki-canary

FROM gcr.io/distroless/static:debug

COPY --from=build /src/loki/cmd/loki-canary/loki-canary /usr/bin/loki-canary
SHELL [ "/busybox/sh", "-c" ]
RUN ln -s /busybox/sh /bin/sh
ENTRYPOINT [ "/usr/bin/loki-canary" ]
