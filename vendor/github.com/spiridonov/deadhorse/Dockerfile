FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/deadhorse ./cmd/deadhorse

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/deadhorse /deadhorse
EXPOSE 9000 9090
ENTRYPOINT ["/deadhorse"]
