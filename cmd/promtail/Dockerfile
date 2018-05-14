FROM alpine:3.4
RUN apk add --update --no-cache ca-certificates && rm /var/cache/apk/*
ADD promtail /usr/bin
ENTRYPOINT ["/usr/bin/promtail"]
