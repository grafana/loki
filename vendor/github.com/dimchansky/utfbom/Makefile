.PHONY: check task test test_race test_cover fmt vet lint fix

# Checks only; never modifies files.
check: vet lint test_race

# Rewrites files first, then checks; sub-makes keep the steps sequential under make -j.
task:
	@$(MAKE) fix
	@$(MAKE) fmt
	@$(MAKE) check

test:
	@go test ./...

test_race:
	@go test -race -shuffle=on ./...

test_cover:
	@go test -cover -covermode=atomic ./...

# tested with golangci-lint v2.15.0 (same version as CI)
fmt:
	@golangci-lint fmt

vet:
	@go vet ./...

lint:
	@golangci-lint run

fix:
	@go fix ./...
