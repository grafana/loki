ARG GO_VERSION=1.26@sha256:670bb5aa5349ae03b8516764718486f4f16399134bdc9acd0c46a23f043ab05c
FROM golang:${GO_VERSION} as build

# build via Makefile target helm-test-image in root
# Makefile. Building from this directory will not be
# able to access source needed in rest of repo.
COPY . /src/loki
WORKDIR /src/loki
RUN make clean && make BUILD_IN_CONTAINER=false helm-test

FROM gcr.io/distroless/static:debug@sha256:63b4c0065ff7bd68753510400b2d5cedd5d5571d15eac4be0b6b0dd236a2d148
COPY --from=build /src/loki/production/helm/loki/src/helm-test/helm-test /usr/bin/helm-test
ENTRYPOINT [ "/usr/bin/helm-test" ]
