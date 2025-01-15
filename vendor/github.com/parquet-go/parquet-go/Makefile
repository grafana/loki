.PHONY: format

AUTHORS.txt: .mailmap
	go install github.com/kevinburke/write_mailmap@latest
	write_mailmap > AUTHORS.txt

format:
	go install github.com/kevinburke/differ@latest
	differ gofmt -w .

test:
	go test -v -trimpath -race -cover -tags= ./...
