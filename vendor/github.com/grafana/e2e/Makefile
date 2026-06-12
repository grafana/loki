.PHONY: test
test:
	go test -tags netgo -timeout 30m -race -count 1 ./...

.PHONY: lint
lint:
	go run github.com/client9/misspell/cmd/misspell@v0.3.4 -error README.md LICENSE

	# Configured via .golangci.yml.
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.8.0 run

.PHONY: integration
integration:
	go test -tags netgo,requires_docker -timeout 30m -v -count=1 ./
