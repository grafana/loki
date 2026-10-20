ARG GO_VERSION=1.26.5@sha256:705e964a93a2fd2e75c7d59bb7d781b57e30f12293ffde5175c69229e18fb678
FROM golang:${GO_VERSION} as build
ARG IMAGE_TAG

COPY . /src/loki
WORKDIR /src/loki
RUN go env GOARCH > /goarch
RUN make clean && make GOARCH=$(cat /goarch) BUILD_IN_CONTAINER=true GOEXPERIMENT=boringcrypto IMAGE_TAG=${IMAGE_TAG} loki-canary-boringcrypto

FROM gcr.io/distroless/base-nossl:debug@sha256:c1e2ae7459b7ef4feee352478930cca7ba08e857398e34dd53d66f4044cc2b2f
COPY --from=build /src/loki/cmd/loki-canary-boringcrypto/loki-canary-boringcrypto /usr/bin/loki-canary
SHELL [ "/busybox/sh", "-c" ]
RUN ln -s /busybox/sh /bin/sh
ENTRYPOINT [ "/usr/bin/loki-canary" ]
