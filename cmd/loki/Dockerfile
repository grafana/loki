FROM       alpine:3.4
RUN        apk add --update --no-cache ca-certificates && rm /var/cache/apk/*
COPY       loki /bin/loki
COPY       loki-local-config.yaml /etc/loki/local-config.yaml
EXPOSE     80
ENTRYPOINT [ "/bin/loki" ]
