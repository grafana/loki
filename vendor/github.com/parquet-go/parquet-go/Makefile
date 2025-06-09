.PHONY: format

AUTHORS.txt: .mailmap
	go install github.com/kevinburke/write_mailmap@latest
	write_mailmap > AUTHORS.txt

tools:
	go mod tidy -modfile go.tools.mod

format: tools
	go fmt ./...
	go tool -modfile go.tools.mod modernize -fix -test ./...

test:
	go test -v -trimpath -race -cover -tags= ./...
