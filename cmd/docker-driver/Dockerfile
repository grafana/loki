FROM       alpine:3.9
RUN        apk add --update --no-cache ca-certificates
COPY       docker-driver /bin/docker-driver
WORKDIR /bin/
ENTRYPOINT [ "/bin/docker-driver" ]