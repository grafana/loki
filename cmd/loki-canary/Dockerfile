ARG GO_VERSION=1.27@sha256:3680233e3204827fbdc66088528ae6d4b3d034f51d03a99d454f6de034888244
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
