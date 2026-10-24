ARG GO_VERSION=1.27@sha256:f20d83a83e18dbd2a4714b8ffd4b30b287b6243962dd75608e86abcc63ba4efc
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
