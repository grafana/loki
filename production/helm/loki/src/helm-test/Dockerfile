ARG GO_VERSION=1.26
FROM golang:${GO_VERSION} as build

# build via Makefile target helm-test-image in root
# Makefile. Building from this directory will not be
# able to access source needed in rest of repo.
COPY . /src/loki
WORKDIR /src/loki
RUN make clean && make BUILD_IN_CONTAINER=false helm-test

FROM gcr.io/distroless/static:debug@sha256:fdd11b2ee83d2cd35774fcb1c94c76e57fd4470da0c26d5748a91fc2faf7b9b1
COPY --from=build /src/loki/production/helm/loki/src/helm-test/helm-test /usr/bin/helm-test
ENTRYPOINT [ "/usr/bin/helm-test" ]
