SHELL := /bin/bash -eu -o pipefail

GO_TEST_FLAGS  := -v

PACKAGE_DIRS := $(shell go list ./... 2> /dev/null | grep -v /vendor/)
SRC_FILES    := $(shell find . -name '*.go' -not -path './vendor/*')


#  Tasks
#-----------------------------------------------
.PHONY: lint
lint:
	@gofmt -e -d -s $(SRC_FILES) | awk '{ e = 1; print $0 } END { if (e) exit(1) }'
	@echo $(SRC_FILES) | xargs -n1 golint -set_exit_status
	@go vet $(PACKAGE_DIRS)

.PHONY: test
test: lint
	@go test $(GO_TEST_FLAGS) $(PACKAGE_DIRS)

.PHONY: ci-test
ci-test: lint
	@echo > coverage.txt
	@for d in $(PACKAGE_DIRS); do \
		go test -coverprofile=profile.out -covermode=atomic -race -v $$d; \
		if [ -f profile.out ]; then \
			cat profile.out >> coverage.txt; \
			rm profile.out; \
		fi; \
	done
