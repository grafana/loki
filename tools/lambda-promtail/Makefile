all: clean test build

GOTEST ?= go test

build:
	GOARCH=amd64 GOOS=linux CGO_ENABLED=0 go build -o ./bootstrap lambda-promtail/*.go

test:
	$(GOTEST) ./...

clean:
	rm -f main
