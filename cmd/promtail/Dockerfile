FROM       alpine:3.4
RUN        apk add --update --no-cache ca-certificates && rm /var/cache/apk/*
ADD        promtail /usr/bin
COPY       promtail-local-config.yaml /etc/promtail/local-config.yaml
COPY       promtail-docker-config.yaml /etc/promtail/docker-config.yaml
ENTRYPOINT ["/usr/bin/promtail"]
