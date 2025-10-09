ARG GO_VERSION=1.24
ARG IMAGE_TAG
FROM golang:${GO_VERSION} AS build

COPY . /src/loki
WORKDIR /src/loki
RUN make clean && make BUILD_IN_CONTAINER=false IMAGE_TAG=${IMAGE_TAG} logcli


FROM gcr.io/distroless/static:nonroot

COPY --from=build /src/loki/cmd/logcli/logcli /usr/bin/logcli

ENTRYPOINT [ "/usr/bin/logcli" ]
