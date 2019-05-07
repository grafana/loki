FROM       alpine:3.9
RUN        apk add --update --no-cache ca-certificates
COPY       loki-debug /bin/loki-debug
ADD        dlv /usr/bin
COPY       loki-local-config.yaml /etc/loki/local-config.yaml
EXPOSE     80

# Expose 40000 for delve
EXPOSE 40000

# Allow delve to run on Alpine based containers.
RUN apk add --no-cache libc6-compat

# Run delve, ending with -- because we pass params via kubernetes, per the docs:
#   Pass flags to the program you are debugging using --, for example:`
#   dlv exec ./hello -- server --config conf/config.toml`
ENTRYPOINT ["/usr/bin/dlv", "--listen=:40000", "--headless=true", "--api-version=2", "exec", "/bin/loki-debug", "--"]
