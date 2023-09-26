all: clean test build

GOTEST ?= go test

build:
	GOOS=linux CGO_ENABLED=0 go build -o ./main lambda-promtail/*.go

test:
	$(GOTEST) ./...

clean:
	rm -f main
