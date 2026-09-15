# Build the manager binary
FROM golang:1.27.1@sha256:f44f6e88636cfb311f9ebace870ded69d943f227bb3cb27d32ffd84ea18c43ea as builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY api/ api/
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY cmd/loki-operator/main.go cmd/loki-operator/main.go
COPY internal/ internal/

# Build
RUN CGO_ENABLED=0 GOOS=linux GO111MODULE=on go build -mod=readonly -o manager cmd/loki-operator/main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
