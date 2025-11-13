undefine GOFLAGS

GOLANGCI_LINT_VERSION?=v1.62.2
GOTESTSUM_VERSION?=v1.12.2
GO_TEST?=go run gotest.tools/gotestsum@$(GOTESTSUM_VERSION) --format testname --
TIMEOUT := "60m"

ifeq ($(shell command -v podman 2> /dev/null),)
	RUNNER=docker
else
	RUNNER=podman
endif

# if the golangci-lint steps fails with one of the following error messages:
#
#   directory prefix . does not contain main module or its selected dependencies
#
#   failed to initialize build cache at /root/.cache/golangci-lint: mkdir /root/.cache/golangci-lint: permission denied
#
# you probably have to fix the SELinux security context for root directory plus your cache
#
#   chcon -Rt svirt_sandbox_file_t .
#   chcon -Rt svirt_sandbox_file_t ~/.cache/golangci-lint
lint:
	mkdir -p ~/.cache/golangci-lint/$(GOLANGCI_LINT_VERSION)
	$(RUNNER) run -t --rm \
		-v $(shell pwd):/app \
		-v ~/.cache/golangci-lint/$(GOLANGCI_LINT_VERSION):/root/.cache \
		-w /app \
		-e GOFLAGS="-tags=acceptance" \
		golangci/golangci-lint:$(GOLANGCI_LINT_VERSION) golangci-lint run -v --max-same-issues 50
.PHONY: lint

format:
	gofmt -w -s $(shell pwd)
.PHONY: format

unit:
	$(GO_TEST) -shuffle on ./...
.PHONY: unit

coverage:
	$(GO_TEST) -shuffle on -covermode count -coverprofile cover.out -coverpkg=./... ./...
.PHONY: coverage

acceptance: acceptance-basic acceptance-baremetal acceptance-blockstorage acceptance-compute acceptance-container acceptance-containerinfra acceptance-db acceptance-dns acceptance-identity acceptance-image acceptance-keymanager acceptance-loadbalancer acceptance-messaging acceptance-networking acceptance-objectstorage acceptance-orchestration acceptance-placement acceptance-sharedfilesystems acceptance-workflow
.PHONY: acceptance

acceptance-basic:
	$(GO_TEST) -timeout $(TIMEOUT) -tags "fixtures acceptance" ./internal/acceptance/openstack
.PHONY: acceptance-basic

acceptance-baremetal:
	$(GO_TEST) -timeout $(TIMEOUT) -tags "fixtures acceptance" ./internal/acceptance/openstack/baremetal/...
.PHONY: acceptance-baremetal

acceptance-blockstorage:
	$(GO_TEST) -timeout $(TIMEOUT) -tags "fixtures acceptance" ./internal/acceptance/openstack/blockstorage/...
.PHONY: acceptance-blockstorage

acceptance-compute:
	$(GO_TEST) -timeout $(TIMEOUT) -tags "fixtures acceptance" ./internal/acceptance/openstack/compute/...
.PHONY: acceptance-compute

acceptance-container:
	$(GO_TEST) -timeout $(TIMEOUT) -tags "fixtures acceptance" ./internal/acceptance/openstack/container/...
.PHONY: acceptance-container

acceptance-containerinfra:
	$(GO_TEST) -timeout $(TIMEOUT) -tags "fixtures acceptance" ./internal/acceptance/openstack/containerinfra/...
.PHONY: acceptance-containerinfra

acceptance-db:
	$(GO_TEST) -timeout $(TIMEOUT) -tags "fixtures acceptance" ./internal/acceptance/openstack/db/...
.PHONY: acceptance-db

acceptance-dns:
	$(GO_TEST) -timeout $(TIMEOUT) -tags "fixtures acceptance" ./internal/acceptance/openstack/dns/...
.PHONY: acceptance-dns

acceptance-identity:
	$(GO_TEST) -timeout $(TIMEOUT) -tags "fixtures acceptance" ./internal/acceptance/openstack/identity/...
.PHONY: acceptance-identity

acceptance-image:
	$(GO_TEST) -timeout $(TIMEOUT) -tags "fixtures acceptance" ./internal/acceptance/openstack/image/...
.PHONY: acceptance-image

acceptance-keymanager:
	$(GO_TEST) -timeout $(TIMEOUT) -tags "fixtures acceptance" ./internal/acceptance/openstack/keymanager/...
.PHONY: acceptance-keymanager

acceptance-loadbalancer:
	$(GO_TEST) -timeout $(TIMEOUT) -tags "fixtures acceptance" ./internal/acceptance/openstack/loadbalancer/...
.PHONY: acceptance-loadbalancer

acceptance-messaging:
	$(GO_TEST) -timeout $(TIMEOUT) -tags "fixtures acceptance" ./internal/acceptance/openstack/messaging/...
.PHONY: acceptance-messaging

acceptance-networking:
	$(GO_TEST) -timeout $(TIMEOUT) -tags "fixtures acceptance" ./internal/acceptance/openstack/networking/...
.PHONY: acceptance-networking

acceptance-objectstorage:
	$(GO_TEST) -timeout $(TIMEOUT) -tags "fixtures acceptance" ./internal/acceptance/openstack/objectstorage/...
.PHONY: acceptance-objectstorage

acceptance-orchestration:
	$(GO_TEST) -timeout $(TIMEOUT) -tags "fixtures acceptance" ./internal/acceptance/openstack/orchestration/...
.PHONY: acceptance-orchestration

acceptance-placement:
	$(GO_TEST) -timeout $(TIMEOUT) -tags "fixtures acceptance" ./internal/acceptance/openstack/placement/...
.PHONY: acceptance-placement

acceptance-sharedfilesystems:
	$(GO_TEST) -timeout $(TIMEOUT) -tags "fixtures acceptance" ./internal/acceptance/openstack/sharedfilesystems/...
.PHONY: acceptance-sharefilesystems

acceptance-workflow:
	$(GO_TEST) -timeout $(TIMEOUT) -tags "fixtures acceptance" ./internal/acceptance/openstack/workflow/...
.PHONY: acceptance-workflow
