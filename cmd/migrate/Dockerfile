FROM golang:1.21.3 as build
COPY . /src/loki
WORKDIR /src/loki
RUN make clean && make BUILD_IN_CONTAINER=false migrate

FROM alpine:3.18.6
RUN apk add --update --no-cache ca-certificates
COPY --from=build /src/loki/cmd/migrate/migrate /usr/bin/migrate
#ENTRYPOINT [ "/usr/bin/migrate" ]
CMD tail -f /dev/null
