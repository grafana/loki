FROM       alpine:3.9
RUN        apk add --update --no-cache ca-certificates
ADD        loki-canary /usr/bin
ENTRYPOINT [ "/usr/bin/loki-canary" ]
