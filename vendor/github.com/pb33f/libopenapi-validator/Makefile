all: gofumpt import lint

init:
	go install mvdan.cc/gofumpt@latest
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	go install github.com/daixiang0/gci@latest

lint:
	golangci-lint run ./...

gofumpt:
	gofumpt -l -w .

import:
	gci write --skip-generated -s standard -s default -s localmodule -s blank -s dot -s alias .
