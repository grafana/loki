FROM       alpine:3.4
RUN        apk add --update --no-cache ca-certificates && rm /var/cache/apk/*
COPY       loki /bin/loki
EXPOSE     80
ENTRYPOINT [ "/bin/loki" ]
