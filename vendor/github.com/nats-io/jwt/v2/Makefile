.PHONY: test cover

build:
	go build

fmt:
	gofmt -w -s *.go
	goimports -w *.go
	go mod tidy

test:
	go vet ./...
	staticcheck ./...
	rm -rf ./coverage.out
	go test -v -coverprofile=./coverage.out ./...

cover:
	 go tool cover -html=coverage.out