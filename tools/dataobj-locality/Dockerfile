ARG GO_VERSION=1.26.4

# Build a statically linked binary so the runtime image needs no OS packages.
FROM golang:${GO_VERSION} AS build

WORKDIR /src/loki
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -tags netgo \
    -ldflags="-s -w" \
    -o /out/dataobj-locality \
    ./tools/dataobj-locality

# Distroless static contains only the files needed by a static binary and runs
# as the unprivileged nonroot user. It also avoids shipping a shell or package
# manager in the reporting workload.
FROM gcr.io/distroless/static:nonroot

COPY --from=build /out/dataobj-locality /usr/bin/dataobj-locality

USER nonroot:nonroot
EXPOSE 9900
ENTRYPOINT ["/usr/bin/dataobj-locality"]
