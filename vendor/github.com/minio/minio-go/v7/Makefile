GOPATH := $(shell go env GOPATH)
TMPDIR := $(shell mktemp -d)

all: checks

.PHONY: examples docs

checks: lint vet test examples functional-test

lint:
	@mkdir -p ${GOPATH}/bin
	@echo "Installing golangci-lint" && curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GOPATH)/bin
	@echo "Running $@ check"
	@GO111MODULE=on ${GOPATH}/bin/golangci-lint cache clean
	@GO111MODULE=on ${GOPATH}/bin/golangci-lint run --timeout=5m --config ./.golangci.yml

vet:
	@GO111MODULE=on go vet ./...
	@echo "Installing staticcheck" && go install honnef.co/go/tools/cmd/staticcheck@latest
	${GOPATH}/bin/staticcheck -tests=false -checks="all,-ST1000,-ST1003,-ST1016,-ST1020,-ST1021,-ST1022,-ST1023,-ST1005"

test:
	@GO111MODULE=on SERVER_ENDPOINT=localhost:9000 ACCESS_KEY=minioadmin SECRET_KEY=minioadmin ENABLE_HTTPS=1 MINT_MODE=full go test -race -v ./...

examples:
	@echo "Building s3 examples"
	@cd ./examples/s3 && $(foreach v,$(wildcard examples/s3/*.go),go build -mod=mod -o ${TMPDIR}/$(basename $(v)) $(notdir $(v)) || exit 1;)
	@echo "Building minio examples"
	@cd ./examples/minio && $(foreach v,$(wildcard examples/minio/*.go),go build -mod=mod -o ${TMPDIR}/$(basename $(v)) $(notdir $(v)) || exit 1;)

functional-test:
	@GO111MODULE=on go build -race functional_tests.go
	@SERVER_ENDPOINT=localhost:9000 ACCESS_KEY=minioadmin SECRET_KEY=minioadmin ENABLE_HTTPS=1 MINT_MODE=full ./functional_tests

functional-test-notls:
	@GO111MODULE=on go build -race functional_tests.go
	@SERVER_ENDPOINT=localhost:9000 ACCESS_KEY=minioadmin SECRET_KEY=minioadmin ENABLE_HTTPS=0 MINT_MODE=full ./functional_tests

clean:
	@echo "Cleaning up all the generated files"
	@find . -name '*.test' | xargs rm -fv
	@find . -name '*~' | xargs rm -fv
