FROM       alpine:3.9
RUN        apk add --update --no-cache ca-certificates
COPY       loki /bin/loki
COPY       loki-local-config.yaml /etc/loki/local-config.yaml
EXPOSE     80
ENTRYPOINT [ "/bin/loki" ]
