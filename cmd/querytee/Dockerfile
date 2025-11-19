ARG GO_VERSION=1.25
FROM golang:${GO_VERSION} AS build
ARG IMAGE_TAG

COPY . /src/loki
WORKDIR /src/loki
RUN make clean && make IMAGE_TAG=${IMAGE_TAG} loki-querytee

FROM gcr.io/distroless/static:debug
COPY --from=build /src/loki/cmd/querytee/querytee /usr/bin/querytee

SHELL [ "/busybox/sh", "-c" ]
RUN ln -s /busybox/sh /bin/sh

ENTRYPOINT [ "/usr/bin/querytee" ]
