FROM       alpine:3.9
RUN        apk add --update --no-cache ca-certificates
ADD        promtail-debug /usr/bin
ADD        dlv /usr/bin
COPY       promtail-local-config.yaml /etc/promtail/local-config.yaml
COPY       promtail-docker-config.yaml /etc/promtail/docker-config.yaml

# Expose 40000 for delve
EXPOSE 40000

# Allow delve to run on Alpine based containers.
RUN apk add --no-cache libc6-compat

# Run delve, ending with -- because we pass params via kubernetes, per the docs:
#   Pass flags to the program you are debugging using --, for example:`
#   dlv exec ./hello -- server --config conf/config.toml`
ENTRYPOINT ["/usr/bin/dlv", "--listen=:40000", "--headless=true", "--api-version=2", "exec", "/usr/bin/promtail-debug", "--"]