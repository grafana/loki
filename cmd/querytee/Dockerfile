ARG GO_VERSION=1.24
FROM golang:${GO_VERSION} as build
ARG IMAGE_TAG

COPY . /src/loki
WORKDIR /src/loki
RUN make clean && make BUILD_IN_CONTAINER=false IMAGE_TAG=${IMAGE_TAG} loki-querytee

FROM gcr.io/distroless/static:nonroot
COPY --from=build /src/loki/cmd/querytee/querytee /usr/bin/querytee

ENTRYPOINT [ "/usr/bin/querytee" ]
