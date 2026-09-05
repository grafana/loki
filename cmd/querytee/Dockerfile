ARG GO_VERSION=1.27@sha256:512690a5660563b57d37ecc31129e7f136e831db2aed24a1dbeb8ad7380dc0fa
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
