ARG GO_VERSION=1.26
FROM golang:${GO_VERSION} AS build
ARG IMAGE_TAG

COPY . /src/loki
WORKDIR /src/loki
RUN make clean && make BUILD_IN_CONTAINER=false IMAGE_TAG=${IMAGE_TAG} lokitool

FROM gcr.io/distroless/static:debug
COPY --from=build /src/loki/cmd/lokitool/lokitool /usr/bin/lokitool

SHELL [ "/busybox/sh", "-c" ]
RUN ln -s /busybox/sh /bin/sh

ENTRYPOINT [ "/usr/bin/lokitool" ]
