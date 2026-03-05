all: gofumpt import lint

init:
	go install mvdan.cc/gofumpt@v0.7.0
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.61.0
	go install github.com/daixiang0/gci@v0.13.5

lint:
	golangci-lint run ./...

gofumpt:
	gofumpt -l -w .

import:
	gci write --skip-generated -s standard -s default -s localmodule -s blank -s dot -s alias .
