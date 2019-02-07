FROM       alpine:3.9
RUN        apk add --update --no-cache ca-certificates
ADD        promtail /usr/bin
COPY       promtail-local-config.yaml /etc/promtail/local-config.yaml
COPY       promtail-docker-config.yaml /etc/promtail/docker-config.yaml
ENTRYPOINT ["/usr/bin/promtail"]
