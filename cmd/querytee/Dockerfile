ARG GO_VERSION=1.22
FROM golang:${GO_VERSION} as build

COPY . /src/loki
WORKDIR /src/loki
RUN make clean && make BUILD_IN_CONTAINER=false loki-querytee

FROM gcr.io/distroless/base-nossl:debug
COPY --from=build /src/loki/cmd/querytee/querytee /usr/bin/querytee

SHELL [ "/busybox/sh", "-c" ]
RUN ln -s /busybox/sh /bin/sh

ENTRYPOINT [ "/usr/bin/querytee" ]
