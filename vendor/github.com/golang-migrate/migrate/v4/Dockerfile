FROM golang:1.12-alpine3.10 AS downloader
ARG VERSION

RUN apk add --no-cache git gcc musl-dev

WORKDIR /go/src/github.com/golang-migrate/migrate

COPY . ./

ENV GO111MODULE=on
ENV DATABASES="postgres mysql redshift cassandra spanner cockroachdb clickhouse mongodb sqlserver firebird"
ENV SOURCES="file go_bindata github github_ee aws_s3 google_cloud_storage godoc_vfs gitlab"

RUN go build -a -o build/migrate.linux-386 -ldflags="-s -w -X main.Version=${VERSION}" -tags "$DATABASES $SOURCES" ./cmd/migrate

FROM alpine:3.10

RUN apk add --no-cache ca-certificates

COPY --from=downloader /go/src/github.com/golang-migrate/migrate/build/migrate.linux-386 /migrate

ENTRYPOINT ["/migrate"]
CMD ["--help"]
