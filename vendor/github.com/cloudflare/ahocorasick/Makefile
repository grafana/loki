GCFLAGS := -B
LDFLAGS :=

.PHONY: install
install:
	@go install -v .

.PHONY: test
test:
	@go test -gcflags='$(GCFLAGS)' -race -ldflags='$(LDFLAGS)' .

.PHONY: bench
bench:
	@go test -gcflags='$(GCFLAGS)' -ldflags='$(LDFLAGS)' -benchmem -bench .
