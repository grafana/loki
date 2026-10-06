FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/deadhorse ./cmd/deadhorse

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/deadhorse /deadhorse
USER nonroot:nonroot
EXPOSE 9000 9090
ENTRYPOINT ["/deadhorse"]
