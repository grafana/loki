ARG GO_VERSION=1.27@sha256:f20d83a83e18dbd2a4714b8ffd4b30b287b6243962dd75608e86abcc63ba4efc
FROM golang:${GO_VERSION} as build
ARG IMAGE_TAG

COPY . /src/loki
WORKDIR /src/loki
RUN go env GOARCH > /goarch
RUN make clean && make GOARCH=$(cat /goarch) BUILD_IN_CONTAINER=true GOEXPERIMENT=boringcrypto IMAGE_TAG=${IMAGE_TAG} loki-canary-boringcrypto

FROM gcr.io/distroless/base-nossl:debug
COPY --from=build /src/loki/cmd/loki-canary-boringcrypto/loki-canary-boringcrypto /usr/bin/loki-canary
SHELL [ "/busybox/sh", "-c" ]
RUN ln -s /busybox/sh /bin/sh
ENTRYPOINT [ "/usr/bin/loki-canary" ]
