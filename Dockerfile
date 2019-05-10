FROM golang
COPY ./ /src/
RUN cd /src && \
    CGO_ENABLED=0 go build -o loki-canary cmd/loki-canary/main.go

FROM       alpine:3.9
RUN        apk add --update --no-cache ca-certificates
COPY       --from=0 /src/loki-canary /bin/loki-canary
ENTRYPOINT [ "/bin/loki-canary" ]