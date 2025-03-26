#     ______           ____                     __          __   _
#    / ____/________ _/ __/___ _____  ____ _   / /   ____  / /__(_)
#   / / __/ ___/ __ `/ /_/ __ `/ __ \/ __ `/  / /   / __ \/ //_/ /
#  / /_/ / /  / /_/ / __/ /_/ / / / / /_/ /  / /___/ /_/ / ,< / /
#  \____/_/   \__,_/_/  \__,_/_/ /_/\__,_/  /_____/\____/_/|_/_/
#
#  Loki Project Makefile

SHELL = /usr/bin/env bash -o pipefail

.DEFAULT_GOAL := all

# Certain aspects of the build are done in containers for consistency (e.g.
# yacc/protobuf generation) If you have the correct tools installed and you
# want to speed up development you can run make BUILD_IN_CONTAINER=false target
# or you can override this with an environment variable.
BUILD_IN_CONTAINER ?= true
CI                 ?= false

# Ensure you run `make release-workflows` after changing this
GO_VERSION         := 1.24.1
# Ensure you run `make IMAGE_TAG=<updated-tag> build-image-push` after changing this
BUILD_IMAGE_TAG    := 0.34.6

IMAGE_TAG          ?= $(shell ./tools/image-tag)
GIT_REVISION       := $(shell git rev-parse --short HEAD)
GIT_BRANCH         := $(shell git rev-parse --abbrev-ref HEAD)

# Golang environment
GOOS               ?= $(shell go env GOOS)
GOHOSTOS           ?= $(shell go env GOHOSTOS)
GOARCH             ?= $(shell go env GOARCH)
GOARM              ?= $(shell go env GOARM)
GOEXPERIMENT       ?= $(shell go env GOEXPERIMENT)

GOTEST             ?= go test

# Build flags
VPREFIX            := github.com/grafana/loki/v3/pkg/util/build
GO_LDFLAGS         := -X $(VPREFIX).Branch=$(GIT_BRANCH) \
                      -X $(VPREFIX).Version=$(IMAGE_TAG) \
                      -X $(VPREFIX).Revision=$(GIT_REVISION) \
                      -X $(VPREFIX).BuildUser=$(shell whoami)@$(shell hostname) \
                      -X $(VPREFIX).BuildDate=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

GO_FLAGS           := -ldflags "-extldflags \"-static\" -s -w $(GO_LDFLAGS)" -tags netgo
DYN_GO_FLAGS       := -ldflags "-s -w $(GO_LDFLAGS)" -tags netgo

# Per some websites I've seen to add `-gcflags "all=-N -l"`, the gcflags seem poorly if at all documented
# the best I could dig up is -N disables optimizations and -l disables inlining which should make debugging match source better.
# Also remove the -s and -w flags present in the normal build which strip the symbol table and the DWARF symbol table.
DEBUG_GO_FLAGS     := -gcflags "all=-N -l" -ldflags "-extldflags \"-static\" $(GO_LDFLAGS)" -tags netgo
DEBUG_DYN_GO_FLAGS := -gcflags "all=-N -l" -ldflags "$(GO_LDFLAGS)" -tags netgo

# Image names
IMAGE_PREFIX           ?= grafana
BUILD_IMAGE            := $(IMAGE_PREFIX)/loki-build-image:$(BUILD_IMAGE_TAG)
LOKI_IMAGE             := $(IMAGE_PREFIX)/loki:$(IMAGE_TAG)
CANARY_IMAGE           := $(IMAGE_PREFIX)/loki-canary:$(IMAGE_TAG)
QUERY_TEE_IMAGE        := $(IMAGE_PREFIX)/loki-query-tee:$(IMAGE_TAG)
LOGCLI_IMAGE           := $(IMAGE_PREFIX)/logcli:$(IMAGE_TAG)
PROMTAIL_IMAGE         := $(IMAGE_PREFIX)/promtail:$(IMAGE_TAG)
LOGQL_ANALYZER_IMAGE   := $(IMAGE_PREFIX)/logql-analyzer:$(IMAGE_TAG)
OPERATOR_IMAGE         := $(IMAGE_PREFIX)/loki-operator:$(IMAGE_TAG)

# OCI (Docker) setup
OCI_PLATFORMS  := --platform=linux/amd64,linux/arm64
OCI_BUILD_ARGS := --build-arg GO_VERSION=$(GO_VERSION) --build-arg BUILD_IMAGE=$(BUILD_IMAGE) --build-arg IMAGE_TAG=$(IMAGE_TAG)
OCI_PUSH_ARGS  := -o type=registry
OCI_PUSH       := docker push
OCI_TAG        := docker tag

ifeq ($(CI),true)
  # ensure buildx is set up
	_               := $(shell ./tools/ensure-buildx-builder.sh)
	OCI_BUILD       := DOCKER_BUILDKIT=1 docker buildx build $(OCI_PLATFORMS) $(OCI_BUILD_ARGS)
else
	OCI_BUILD       := DOCKER_BUILDKIT=1 docker build $(OCI_BUILD_ARGS)
endif

BUILD_OCI_PUSH  := $(OCI_BUILD) $(OCI_PUSH_ARGS)

# Docker mount flag, ignored on native docker host.
# See https://docs.docker.com/docker-for-mac/osxfs-caching/#delegated
MOUNT_FLAGS    := :delegated

define run_in_container
	@mkdir -p $(shell pwd)/.pkg $(shell pwd)/.cache
	@echo ">>> Running make $@ in container ..."
	docker run --rm --tty --interactive \
		-v $(shell go env GOPATH)/pkg:/go/pkg$(MOUNT_FLAGS) \
		-v $(shell pwd)/.cache:/go/cache$(MOUNT_FLAGS) \
		-v $(shell pwd):/src/loki$(MOUNT_FLAGS) \
		$(BUILD_IMAGE) -f Makefile $@;
endef

# Adapted from https://www.thapaliya.com/en/writings/well-documented-makefiles/
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-45s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.PHONY: all images check-generated-files logcli loki loki-debug promtail promtail-debug loki-canary loki-canary-boringcrypto lint test clean yacc protos touch-protobuf-sources
.PHONY: format check-format
.PHONY: docker-driver docker-driver-clean docker-driver-enable docker-driver-push
.PHONY: fluent-bit-image, fluent-bit-test
.PHONY: fluentd-image, fluentd-test
.PHONY: promtail-image loki-image build-image build-image-push
.PHONY: bigtable-backup, push-bigtable-backup
.PHONY: benchmark-store, check-mod
.PHONY: migrate migrate-image lint-markdown ragel
.PHONY: doc check-doc
.PHONY: validate-example-configs generate-example-config-doc check-example-config-doc
.PHONY: clean clean-protos
.PHONY: k3d-loki k3d-enterprise-logs k3d-down
.PHONY: helm-test helm-lint

#############
# Variables #
#############

DOCKER_IMAGE_DIRS := $(patsubst %/Dockerfile,%,$(DOCKERFILES))

# We don't want find to scan inside a bunch of directories, to accelerate the
# 'make: Entering directory '/src/loki' phase.
DONT_FIND := -name tools -prune -o -name vendor -prune -o -name operator -prune -o -name .git -prune -o -name .cache -prune -o -name .pkg -prune -o

# Protobuf files
PROTO_DEFS := $(shell find . $(DONT_FIND) -type f -name '*.proto' -print)
PROTO_GOS := $(patsubst %.proto,%.pb.go,$(PROTO_DEFS))

# Yacc Files
YACC_DEFS := $(shell find . $(DONT_FIND) -type f -name *.y -print)
YACC_GOS := $(patsubst %.y,%.y.go,$(YACC_DEFS))

# Ragel Files
RAGEL_DEFS := $(shell find . $(DONT_FIND) -type f -name *.rl -print)
RAGEL_GOS := $(patsubst %.rl,%.rl.go,$(RAGEL_DEFS))

# Promtail UI files
PROMTAIL_GENERATED_FILE := clients/pkg/promtail/server/ui/assets_vfsdata.go
PROMTAIL_UI_FILES := $(shell find ./clients/pkg/promtail/server/ui -type f -name assets_vfsdata.go -prune -o -print)

# Documentation source path
DOC_SOURCES_PATH := docs/sources
DOC_TEMPLATE_PATH := docs/templates

# Configuration flags documentation
DOC_FLAGS_TEMPLATE := $(DOC_TEMPLATE_PATH)/configuration.template
DOC_FLAGS := $(DOC_SOURCES_PATH)/shared/configuration.md

##########
# Docker #
##########

binfmt:
	docker run --privileged linuxkit/binfmt:v0.6

################
# Main Targets #
################
all: promtail logcli loki loki-canary ## build all executables (loki, logcli, promtail, loki-canary)

# This is really a check for the CI to make sure generated files are built and checked in manually
check-generated-files: yacc ragel fmt-proto protos clients/pkg/promtail/server/ui/assets_vfsdata.go
	@if ! (git diff --exit-code $(YACC_GOS) $(RAGEL_GOS) $(PROTO_DEFS) $(PROTO_GOS) $(PROMTAIL_GENERATED_FILE)); then \
		echo "\nChanges found in generated files"; \
		echo "Run 'make check-generated-files' and commit the changes to fix this error."; \
		echo "If you are actively developing these files you can ignore this error"; \
		echo "(Don't forget to check in the generated files when finished)\n"; \
		exit 1; \
	fi

##########
# Logcli #
##########
.PHONY: cmd/logcli/logcli
logcli: cmd/logcli/logcli ## build logcli executable
logcli-debug: cmd/logcli/logcli-debug ## build debug logcli executable

logcli-image: ## build logcli docker image
	$(OCI_BUILD) -t $(LOGCLI_IMAGE) -f cmd/logcli/Dockerfile .

cmd/logcli/logcli:
	CGO_ENABLED=0 go build $(GO_FLAGS) -o $@ ./cmd/logcli

cmd/logcli/logcli-debug:
	CGO_ENABLED=0 go build $(DEBUG_GO_FLAGS) -o ./cmd/logcli/logcli-debug ./cmd/logcli

########
# Loki #
########
.PHONY: cmd/loki/loki cmd/loki/loki-debug
loki: cmd/loki/loki ## build loki executable
loki-debug: cmd/loki/loki-debug ## build loki debug executable

cmd/loki/loki:
	CGO_ENABLED=0 go build $(GO_FLAGS) -o $@ ./$(@D)

cmd/loki/loki-debug:
	CGO_ENABLED=0 go build $(DEBUG_GO_FLAGS) -o $@ ./$(@D)

ui-assets:
	make -C pkg/ui/frontend build
###############
# Loki-Canary #
###############
.PHONY: cmd/loki-canary/loki-canary
loki-canary: cmd/loki-canary/loki-canary ## build loki-canary executable

cmd/loki-canary/loki-canary:
	CGO_ENABLED=0 go build $(GO_FLAGS) -o $@ ./$(@D)

###############
# Loki-Canary (BoringCrypto)#
###############
.PHONY: cmd/loki-canary-boringcrypto/loki-canary-boringcrypto
loki-canary-boringcrypto: cmd/loki-canary-boringcrypto/loki-canary-boringcrypto ## build loki-canary (BoringCrypto) executable

cmd/loki-canary-boringcrypto/loki-canary-boringcrypto:
	CGO_ENABLED=1 GOOS=linux GOARCH=$(GOARCH) GOEXPERIMENT=boringcrypto go build $(GO_FLAGS) -o $@ ./$(@D)/../loki-canary

###############
# Helm #
###############
.PHONY: production/helm/loki/src/helm-test/helm-test
helm-test: production/helm/loki/src/helm-test/helm-test ## run helm tests

# Package Helm tests but do not run them.
production/helm/loki/src/helm-test/helm-test:
	CGO_ENABLED=0 go test $(GO_FLAGS) --tags=helm_test -c -o $@ ./$(@D)

helm-lint: ## run helm linter
	$(MAKE) -BC production/helm/loki lint

helm-docs: ## generate reference documentation
	$(MAKE) -BC docs sources/setup/install/helm/reference.md

#################
# Loki-QueryTee #
#################
.PHONY: cmd/querytee/querytee
loki-querytee: cmd/querytee/querytee ## build loki-querytee executable

cmd/querytee/querytee:
	CGO_ENABLED=0 go build $(GO_FLAGS) -o $@ ./$(@D)

############
# lokitool #
############
.PHONY: cmd/lokitool/lokitool
lokitool: cmd/lokitool/lokitool ## build lokitool executable

cmd/lokitool/lokitool:
	CGO_ENABLED=0 go build $(GO_FLAGS) -o $@ ./cmd/lokitool

############
# Promtail #
############

PROMTAIL_CGO := 0
PROMTAIL_GO_FLAGS := $(GO_FLAGS)
PROMTAIL_DEBUG_GO_FLAGS := $(DEBUG_GO_FLAGS)

# Validate GOHOSTOS=linux && GOOS=linux to use CGO.
ifeq ($(GOHOSTOS),linux)
ifeq ($(GOOS),linux)
ifneq ($(CGO_ENABLED), 0)
PROMTAIL_CGO = 1
endif
PROMTAIL_GO_FLAGS = $(DYN_GO_FLAGS)
PROMTAIL_DEBUG_GO_FLAGS = $(DEBUG_DYN_GO_FLAGS)
endif
endif
ifeq ($(PROMTAIL_JOURNAL_ENABLED), true)
PROMTAIL_GO_TAGS = promtail_journal_enabled
endif
.PHONY: clients/cmd/promtail/promtail clients/cmd/promtail/promtail-debug
promtail: clients/cmd/promtail/promtail ## build promtail executable
promtail-debug: clients/cmd/promtail/promtail-debug ## build debug promtail executable

promtail-clean-assets:
	rm -rf clients/pkg/promtail/server/ui/assets_vfsdata.go

# Rule to generate promtail static assets file
$(PROMTAIL_GENERATED_FILE): $(PROMTAIL_UI_FILES)
	@echo ">> writing assets"
	GOOS=$(shell go env GOHOSTOS) go generate -x -v ./clients/pkg/promtail/server/ui

clients/cmd/promtail/promtail:
	CGO_ENABLED=$(PROMTAIL_CGO) go build $(PROMTAIL_GO_FLAGS) --tags=$(PROMTAIL_GO_TAGS) -o $@ ./$(@D)

clients/cmd/promtail/promtail-debug:
	CGO_ENABLED=$(PROMTAIL_CGO) go build $(PROMTAIL_DEBUG_GO_FLAGS) --tags=$(PROMTAIL_GO_TAGS) -o $@ ./$(@D)

#########
# Mixin #
#########

MIXIN_PATH := production/loki-mixin
MIXIN_OUT_PATH := production/loki-mixin-compiled
MIXIN_OUT_PATH_SSD := production/loki-mixin-compiled-ssd

loki-mixin: ## compile the loki mixin
ifeq ($(BUILD_IN_CONTAINER),true)
	$(run_in_container)
else
	@rm -rf $(MIXIN_OUT_PATH) && mkdir $(MIXIN_OUT_PATH)
	@cd $(MIXIN_PATH) && jb install
	@mixtool generate all --output-alerts $(MIXIN_OUT_PATH)/alerts.yaml --output-rules $(MIXIN_OUT_PATH)/rules.yaml --directory $(MIXIN_OUT_PATH)/dashboards ${MIXIN_PATH}/mixin.libsonnet

	@rm -rf $(MIXIN_OUT_PATH_SSD) && mkdir $(MIXIN_OUT_PATH_SSD)
	@cd $(MIXIN_PATH) && jb install
	@mixtool generate all --output-alerts $(MIXIN_OUT_PATH_SSD)/alerts.yaml --output-rules $(MIXIN_OUT_PATH_SSD)/rules.yaml --directory $(MIXIN_OUT_PATH_SSD)/dashboards ${MIXIN_PATH}/mixin-ssd.libsonnet
endif

loki-mixin-check: loki-mixin ## check the loki mixin is up to date
	@echo "Checking diff"
	@git diff --exit-code -- $(MIXIN_OUT_PATH) || (echo "Please build mixin by running 'make loki-mixin'" && false)
	@git diff --exit-code -- $(MIXIN_OUT_PATH_SSD) || (echo "Please build mixin by running 'make loki-mixin'" && false)

###############
# Migrate #
###############
.PHONY: cmd/migrate/migrate
migrate: cmd/migrate/migrate

cmd/migrate/migrate:
	CGO_ENABLED=0 go build $(GO_FLAGS) -o $@ ./$(@D)

#############
# Releasing #
#############
GOX = gox $(GO_FLAGS) -output="dist/{{.Dir}}-{{.OS}}-{{.Arch}}"
CGO_GOX = gox $(DYN_GO_FLAGS) -cgo -output="dist/{{.Dir}}-{{.OS}}-{{.Arch}}"

SKIP_ARM ?= false
dist: clean
ifeq ($(SKIP_ARM),true)
	CGO_ENABLED=0 $(GOX) -osarch="linux/amd64 darwin/amd64 windows/amd64 freebsd/amd64" ./cmd/loki
	CGO_ENABLED=0 $(GOX) -osarch="linux/amd64 darwin/amd64 windows/amd64 freebsd/amd64" ./cmd/logcli
	CGO_ENABLED=0 $(GOX) -osarch="linux/amd64 darwin/amd64 windows/amd64 freebsd/amd64" ./cmd/loki-canary
	CGO_ENABLED=0 $(GOX) -osarch="linux/amd64 darwin/amd64 windows/amd64 freebsd/amd64" ./cmd/lokitool
	CGO_ENABLED=0 $(GOX) -osarch="darwin/amd64 windows/amd64 windows/386 freebsd/amd64" ./clients/cmd/promtail
	CGO_ENABLED=1 $(CGO_GOX)  -tags promtail_journal_enabled  -osarch="linux/amd64" ./clients/cmd/promtail
else
	CGO_ENABLED=0 $(GOX) -osarch="linux/amd64 linux/arm64 linux/arm darwin/amd64 darwin/arm64 windows/amd64 freebsd/amd64" ./cmd/loki
	CGO_ENABLED=0 $(GOX) -osarch="linux/amd64 linux/arm64 linux/arm darwin/amd64 darwin/arm64 windows/amd64 freebsd/amd64" ./cmd/logcli
	CGO_ENABLED=0 $(GOX) -osarch="linux/amd64 linux/arm64 linux/arm darwin/amd64 darwin/arm64 windows/amd64 freebsd/amd64" ./cmd/loki-canary
	CGO_ENABLED=0 $(GOX) -osarch="linux/amd64 linux/arm64 linux/arm darwin/amd64 darwin/arm64 windows/amd64 freebsd/amd64" ./cmd/lokitool
	CGO_ENABLED=0 $(GOX) -osarch="darwin/amd64 darwin/arm64 windows/amd64 windows/386 freebsd/amd64" ./clients/cmd/promtail
	PKG_CONFIG_PATH="/usr/lib/aarch64-linux-gnu/pkgconfig" CC="aarch64-linux-gnu-gcc" $(CGO_GOX)  -tags promtail_journal_enabled  -osarch="linux/arm64" ./clients/cmd/promtail
	PKG_CONFIG_PATH="/usr/lib/arm-linux-gnueabihf/pkgconfig" CC="arm-linux-gnueabihf-gcc" $(CGO_GOX)  -tags promtail_journal_enabled  -osarch="linux/arm" ./clients/cmd/promtail
	CGO_ENABLED=1 $(CGO_GOX)  -tags promtail_journal_enabled  -osarch="linux/amd64" ./clients/cmd/promtail
endif
	for i in dist/*; do zip -j -m $$i.zip $$i; done
	pushd dist && sha256sum * > SHA256SUMS && popd

packages: dist
	@tools/packaging/nfpm.sh

publish: packages
	./tools/release

########
# Lint #
########

# To run this efficiently on your workstation, run this from the root dir:
# docker run --rm --tty -i -v $(pwd)/.cache:/go/cache -v $(pwd)/.pkg:/go/pkg -v $(pwd):/src/loki grafana/loki-build-image:0.24.1 lint
lint: ## run linters
ifeq ($(BUILD_IN_CONTAINER),true)
	$(run_in_container)
else
	go version
	golangci-lint version
	GO111MODULE=on golangci-lint run -v --timeout 15m --build-tags linux,promtail_journal_enabled
	faillint -paths "sync/atomic=go.uber.org/atomic" ./...
endif

########
# Test #
########

test: all ## run the unit tests
	$(GOTEST) -covermode=atomic -coverprofile=coverage.txt -p=4 ./... | tee test_results.txt
	cd tools/lambda-promtail/ && $(GOTEST) -covermode=atomic -coverprofile=lambda-promtail-coverage.txt -p=4 ./... | tee lambda_promtail_test_results.txt

test-integration:
	$(GOTEST) -count=1 -v -tags=integration -timeout 15m ./integration

compare-coverage:
	./tools/diff_coverage.sh $(old) $(new) $(packages)

#########
# Clean #
#########

clean-protos:
	rm -rf $(PROTO_GOS)

clean: ## clean the generated files
	rm -rf .cache
	rm -rf clients/cmd/docker-driver/rootfs
	rm -rf clients/cmd/fluent-bit/out_grafana_loki.h
	rm -rf clients/cmd/fluent-bit/out_grafana_loki.so
	rm -rf clients/cmd/promtail/promtail
	rm -rf cmd/logcli/logcli
	rm -rf cmd/logql-analyzer/logql-analyzer
	rm -rf cmd/loki-canary/loki-canary
	rm -rf cmd/loki/loki
	rm -rf cmd/migrate/migrate
	rm -rf cmd/querytee/querytee
	rm -rf dist/
	$(MAKE) -BC clients/cmd/fluentd $@
	go clean ./...

#########
# YACCs #
#########

yacc: $(YACC_GOS)

%.y.go: %.y
ifeq ($(BUILD_IN_CONTAINER),true)
	$(run_in_container)
else
	goyacc -l -p $(basename $(notdir $<)) -o $@ $<
endif

#########
# Ragels #
#########

ragel: $(RAGEL_GOS)

%.rl.go: %.rl
ifeq ($(BUILD_IN_CONTAINER),true)
	$(run_in_container)
else
	ragel -Z $< -o $@
endif

#############
# Protobufs #
#############

protos: clean-protos $(PROTO_GOS)

%.pb.go:
ifeq ($(BUILD_IN_CONTAINER),true)
	$(run_in_container)
else
	@# The store-gateway RPC is based on Thanos which uses relative references to other protos, so we need
	@# to configure all such relative paths. `gogo/protobuf` is used by it.
	case "$@" in	\
		vendor*)			\
			protoc -I ./vendor/github.com/gogo/protobuf:./vendor:./$(@D) --gogoslick_out=plugins=grpc:./vendor ./$(patsubst %.pb.go,%.proto,$@); \
			;;					\
		*)						\
			protoc -I .:./vendor/github.com/gogo/protobuf:./vendor/github.com/thanos-io/thanos/pkg:./vendor:./$(@D) --gogoslick_out=Mgoogle/protobuf/timestamp.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/any.proto=github.com/gogo/protobuf/types,plugins=grpc,paths=source_relative:./ ./$(patsubst %.pb.go,%.proto,$@); \
			;;					\
		esac
endif


#################
# Docker Driver #
#################

# optionally set the tag or the arch suffix (-arm64)
LOKI_DOCKER_DRIVER ?= "grafana/loki-docker-driver"
PLUGIN_TAG ?= $(IMAGE_TAG)
PLUGIN_ARCH ?=
PLUGIN_BUILD_ARGS ?=
ifeq ("$(PLUGIN_ARCH)", "-arm64")
	PLUGIN_BUILD_ARGS = --build-arg GOARCH=arm64
endif

# build-rootfs
# builds the plugin rootfs
define build-rootfs
	rm -rf clients/cmd/docker-driver/rootfs || true
	mkdir clients/cmd/docker-driver/rootfs
	$(OCI_BUILD) $(PLUGIN_BUILD_ARGS) -t rootfsimage -f clients/cmd/docker-driver/Dockerfile .

	ID=$$(docker create rootfsimage true) && \
	(docker export $$ID | tar -x -C clients/cmd/docker-driver/rootfs) && \
	docker rm -vf $$ID

	docker rmi rootfsimage -f
endef

docker-driver: docker-driver-clean ## build the docker-driver executable
	$(build-rootfs)
	docker plugin create $(LOKI_DOCKER_DRIVER):$(PLUGIN_TAG)$(PLUGIN_ARCH) clients/cmd/docker-driver

	$(build-rootfs)
	docker plugin create $(LOKI_DOCKER_DRIVER):main$(PLUGIN_ARCH) clients/cmd/docker-driver

clients/cmd/docker-driver/docker-driver:
	CGO_ENABLED=0 go build $(GO_FLAGS) -o $@ ./$(@D)

docker-driver-push: docker-driver
ifndef DOCKER_PASSWORD
	$(error env var DOCKER_PASSWORD is undefined)
endif
ifndef DOCKER_USERNAME
	$(error env var DOCKER_USERNAME is undefined)
endif
	echo ${DOCKER_PASSWORD} | docker login --username ${DOCKER_USERNAME} --password-stdin
	docker plugin push $(LOKI_DOCKER_DRIVER):$(PLUGIN_TAG)$(PLUGIN_ARCH)
	docker plugin push $(LOKI_DOCKER_DRIVER):main$(PLUGIN_ARCH)

docker-driver-enable:
	docker plugin enable $(LOKI_DOCKER_DRIVER):$(PLUGIN_TAG)$(PLUGIN_ARCH)

docker-driver-clean:
	-docker plugin disable $(LOKI_DOCKER_DRIVER):$(PLUGIN_TAG)$(PLUGIN_ARCH)
	-docker plugin rm $(LOKI_DOCKER_DRIVER):$(PLUGIN_TAG)$(PLUGIN_ARCH)
	-docker plugin rm $(LOKI_DOCKER_DRIVER):main$(PLUGIN_ARCH)
	rm -rf clients/cmd/docker-driver/rootfs

#####################
# fluent-bit plugin #
#####################

fluent-bit-plugin: ## build the fluent-bit plugin
	go build $(DYN_GO_FLAGS) -buildmode=c-shared -o clients/cmd/fluent-bit/out_grafana_loki.so ./clients/cmd/fluent-bit/

fluent-bit-image: ## build the fluent-bit plugin docker image
	$(OCI_BUILD) -t $(IMAGE_PREFIX)/fluent-bit-plugin-loki:$(IMAGE_TAG) --build-arg LDFLAGS="-s -w $(GO_LDFLAGS)" -f clients/cmd/fluent-bit/Dockerfile .

fluent-bit-test: LOKI_URL ?= http://localhost:3100/loki/api/
fluent-bit-test:
	docker run -v /var/log:/var/log -e LOG_PATH="/var/log/*.log" -e LOKI_URL="$(LOKI_URL)" $(IMAGE_PREFIX)/fluent-bit-plugin-loki:$(IMAGE_TAG)


##################
# fluentd plugin #
##################

fluentd-plugin: ## build the fluentd plugin
	$(MAKE) -BC clients/cmd/fluentd $@

fluentd-plugin-push: ## push the fluentd plugin
	$(MAKE) -BC clients/cmd/fluentd $@

fluentd-image: ## build the fluentd docker image
	$(OCI_BUILD) -t $(IMAGE_PREFIX)/fluent-plugin-loki:$(IMAGE_TAG) -f clients/cmd/fluentd/Dockerfile .

fluentd-test: LOKI_URL ?= http://loki:3100
fluentd-test:
	LOKI_URL="$(LOKI_URL)" docker-compose -f clients/cmd/fluentd/docker/docker-compose.yml up --build

###################
# logstash plugin #
###################

logstash-image: ## build the logstash image
	$(OCI_BUILD) -t $(IMAGE_PREFIX)/logstash-output-loki:$(IMAGE_TAG) -f clients/cmd/logstash/Dockerfile ./

# Send 10 lines to the local Loki instance.
logstash-push-test-logs: LOKI_URL ?= http://host.docker.internal:3100/loki/api/v1/push
logstash-push-test-logs:
	docker run -e LOKI_URL="$(LOKI_URL)" -v `pwd`/clients/cmd/logstash/loki-test.conf:/home/logstash/loki.conf --rm \
		$(IMAGE_PREFIX)/logstash-output-loki:$(IMAGE_TAG) -f loki.conf

# Enter an env already configure to build and test logstash output plugin.
logstash-env:
	 docker run -v  `pwd`/clients/cmd/logstash:/home/logstash/ -it --rm --entrypoint /bin/sh \
		 $(IMAGE_PREFIX)/logstash-output-loki:$(IMAGE_TAG)

########################
# Bigtable Backup Tool #
########################

BIGTABLE_BACKUP_TOOL_FOLDER = ./tools/bigtable-backup
BIGTABLE_BACKUP_TOOL_TAG ?= $(IMAGE_TAG)

bigtable-backup:
	$(OCI_BUILD) -t $(IMAGE_PREFIX)/$(shell basename $(BIGTABLE_BACKUP_TOOL_FOLDER)) $(BIGTABLE_BACKUP_TOOL_FOLDER)
	$(OCI_TAG) $(IMAGE_PREFIX)/$(shell basename $(BIGTABLE_BACKUP_TOOL_FOLDER)) $(IMAGE_PREFIX)/loki-bigtable-backup:$(BIGTABLE_BACKUP_TOOL_TAG)

push-bigtable-backup: bigtable-backup
	$(OCI_PUSH) $(IMAGE_PREFIX)/loki-bigtable-backup:$(BIGTABLE_BACKUP_TOOL_TAG)

##########
# Images #
##########

images: promtail-image loki-image loki-canary-image helm-test-image docker-driver

# Promtail image
promtail-image: ## build the promtail docker image
	$(OCI_BUILD) -t $(PROMTAIL_IMAGE) -f clients/cmd/promtail/Dockerfile .
promtail-debug-image: ## build the promtail debug docker image
	$(OCI_BUILD) -t $(PROMTAIL_IMAGE)-debug -f clients/cmd/promtail/Dockerfile.debug .

# Loki image
loki-image: ## build the loki docker image
	$(OCI_BUILD) -t $(LOKI_IMAGE) -f cmd/loki/Dockerfile .
loki-debug-image: ## build the loki debug docker image
	$(OCI_BUILD) -t $(LOKI_IMAGE)-debug -f cmd/loki/Dockerfile.debug .

# Loki local image
# Default architecture for local builds
LOCAL_ARCH ?= linux/amd64
loki-local-image: ## build the loki docker image locally (set LOCAL_ARCH=linux/arm64 for arm64)
	docker buildx build --load --platform=$(LOCAL_ARCH) -t $(LOKI_IMAGE) -f cmd/loki/Dockerfile .

# Canary image
loki-canary-image: ## build the canary docker image
	$(OCI_BUILD) -t $(CANARY_IMAGE) -f cmd/loki-canary/Dockerfile .
loki-canary-boringcrypto-image:
	$(OCI_BUILD) -t $(IMAGE_PREFIX)/loki-canary-boringcrypto:$(IMAGE_TAG) -f cmd/loki-canary-boringcrypto/Dockerfile .

# Helm test image
helm-test-image: ## build the helm test docker image
	$(OCI_BUIILD) -t $(IMAGE_PREFIX)/loki-helm-test:$(IMAGE_TAG) -f production/helm/loki/src/helm-test/Dockerfile .
helm-test-push: helm-test-image
	$(OCI_PUSH) $(IMAGE_PREFIX)/loki-helm-test:$(IMAGE_TAG)

# Query Tee image
loki-querytee-image: ## build the querytee docker image
	$(OCI_BUILD) -t $(QUERY_TEE_IMAGE) -f cmd/querytee/Dockerfile .

# Migrate image
migrate-image: ## build the migrate docker image
	$(OCI_BUILD) -t $(IMAGE_PREFIX)/loki-migrate:$(IMAGE_TAG) -f cmd/migrate/Dockerfile .

# LogQL Analyzer
logql-analyzer-image: ## build the logql analyzer docker image
	$(OCI_BUILD) -t $(LOGQL_ANALYZER_IMAGE) -f cmd/logql-analyzer/Dockerfile .

# Build image
build-image: ## build the build docker image
	$(OCI_BUILD) -t $(BUILD_IMAGE) ./loki-build-image
build-image-push:
ifneq (,$(findstring WIP,$(IMAGE_TAG)))
	@echo "Cannot push a WIP image, commit changes first"; \
	false;
endif
	DOCKER_BUILDKIT=1 docker buildx build $(OCI_PLATFORMS) $(OCI_BUILD_ARGS) $(OCI_PUSH_ARGS) -t $(BUILD_IMAGE) ./loki-build-image

# Loki Operator
loki-operator-image: ## build the operator docker image
	$(OCI_BUILD) -t $(OPERATOR_IMAGE) -f operator/Dockerfile ./operator

#################
# Documentation #
#################

documentation-helm-reference-check:
	@echo "Checking diff"
	$(MAKE) -BC docs sources/setup/install/helm/reference.md
	@git diff --exit-code -- docs/sources/setup/install/helm/reference.md || (echo "Please generate Helm Chart reference by running 'make -C docs sources/setup/install/helm/reference.md'" && false)

########
# Misc #
########

benchmark-store:
	go run ./pkg/storage/hack/main.go
	$(GOTEST) ./pkg/storage/ -bench=.  -benchmem -memprofile memprofile.out -cpuprofile cpuprofile.out -trace trace.out

# support go modules
check-mod:
ifeq ($(BUILD_IN_CONTAINER),true)
	$(run_in_container)
else
	GO111MODULE=on GOPROXY=https://proxy.golang.org go mod download
	GO111MODULE=on GOPROXY=https://proxy.golang.org go mod verify
	GO111MODULE=on GOPROXY=https://proxy.golang.org go mod tidy
	GO111MODULE=on GOPROXY=https://proxy.golang.org go mod vendor
endif
	@git diff --exit-code -- go.sum go.mod vendor/ || \
	    (echo "Run 'go mod download && go mod verify && go mod tidy && go mod vendor' and check in changes to vendor/ to fix failed check-mod."; exit 1)


lint-jsonnet:
	@RESULT=0; \
	for f in $$(find . -name 'vendor' -prune -o -name '*.libsonnet' -print -o -name '*.jsonnet' -print); do \
		jsonnetfmt -- "$$f" | diff -u "$$f" -; \
		RESULT=$$(($$RESULT + $$?)); \
	done; \
	for d in $$(find . -name '*-mixin' -a -type d -print); do \
		if [ -e "$$d/jsonnetfile.json" ]; then \
			echo "Installing dependencies for $$d"; \
			pushd "$$d" >/dev/null && jb install && popd >/dev/null; \
		fi; \
	done; \
	for m in $$(find . -name 'mixin.libsonnet' -not -path '*/vendor/*' -print); do \
			echo "Linting $$m"; \
			mixtool lint -J $$(dirname "$$m")/vendor "$$m"; \
			if [ $$? -ne 0 ]; then \
				RESULT=1; \
			fi; \
	done; \
	exit $$RESULT

fmt-jsonnet:
	@find . -name 'vendor' -prune -o -name '*.libsonnet' -print -o -name '*.jsonnet' -print | \
		xargs -n 1 -- jsonnetfmt -i

fmt-proto:
ifeq ($(BUILD_IN_CONTAINER),true)
	$(run_in_container)
else
	echo '$(PROTO_DEFS)' | \
		xargs -n 1 -- buf format -w
endif


lint-scripts:
    # Ignore https://github.com/koalaman/shellcheck/wiki/SC2312
	@find . -name '*.sh' -not -path '*/vendor/*' -print0 | \
		xargs -0 -n1 shellcheck -e SC2312 -x -o all


# search for dead link in our documentation.
# To avoid being rate limited by Github you can use an env variable GITHUB_TOKEN to pass a github token API.
# see https://github.com/settings/tokens
lint-markdown:
ifeq ($(BUILD_IN_CONTAINER),true)
	$(run_in_container)
else
	lychee --verbose --config .lychee.toml ./*.md  ./docs/**/*.md  ./production/**/*.md ./cmd/**/*.md ./clients/**/*.md ./tools/**/*.md
endif


# usage: FUZZ_TESTCASE_PATH=/tmp/testcase make test-fuzz
# this will run the fuzzing using /tmp/testcase and save benchmark locally.
test-fuzz:
	$(GOTEST) -timeout 30s -tags dev,gofuzz -cpuprofile cpu.prof -memprofile mem.prof  \
		-run ^Test_Fuzz$$ github.com/grafana/loki/pkg/logql/syntax -v -count=1 -timeout=0s

format:
	find . $(DONT_FIND) -name '*.pb.go' -prune -o -name '*.y.go' -prune -o -name '*.rl.go' -prune -o \
		-name '*_vfsdata.go' -prune -o -type f -name '*.go' -exec gofmt -w -s {} \;
	find . $(DONT_FIND) -name '*.pb.go' -prune -o -name '*.y.go' -prune -o -name '*.rl.go' -prune -o \
		-name '*_vfsdata.go' -prune -o -type f -name '*.go' -exec goimports -w -local github.com/grafana/loki {} \;


GIT_TARGET_BRANCH ?= main
check-format: format
	git diff --name-only HEAD origin/$(GIT_TARGET_BRANCH) -- "*.go" | xargs --no-run-if-empty git diff --exit-code -- \
	|| (echo "Please format code by running 'make format' and committing the changes" && false)

# Documentation related commands

doc: ## Generates the config file documentation
ifeq ($(BUILD_IN_CONTAINER),true)
	$(run_in_container)
else
	go run ./tools/doc-generator $(DOC_FLAGS_TEMPLATE) > $(DOC_FLAGS)
endif

docs: doc

check-doc: ## Check the documentation files are up to date
check-doc: doc
	@find . -name "*.md" | xargs git diff --exit-code -- \
	|| (echo "Please update generated documentation by running 'make doc' and committing the changes" && false)

###################
# Example Configs #
###################
EXAMPLES_DOC_PATH := $(DOC_SOURCES_PATH)/configure/examples
EXAMPLES_DOC_OUTPUT_PATH := $(EXAMPLES_DOC_PATH)/configuration-examples.md
EXAMPLES_YAML_PATH := $(EXAMPLES_DOC_PATH)/yaml
EXAMPLES_SKIP_VALIDATION_FLAG := "doc-example:skip-validation=true"

# Validate the example configurations that we provide in ./docs/sources/configure/examples
# We run the validation only for complete examples, not snippets.
# Complete examples should contain "Example" in their file name.
validate-example-configs: loki
	for f in $$(grep -rL $(EXAMPLES_SKIP_VALIDATION_FLAG) $(EXAMPLES_YAML_PATH)/*.yaml); do echo "Validating provided example config: $$f" && ./cmd/loki/loki -config.file=$$f -verify-config || exit 1; done

validate-dev-cluster-config: loki
	./cmd/loki/loki -config.file=./tools/dev/loki-tsdb-storage-s3/config/loki.yaml -verify-config

# Dynamically generate ./docs/sources/configure/examples.md using the example configs that we provide.
# This target should be run if any of our example configs change.
generate-example-config-doc:
	echo "Removing existing doc at $(EXAMPLES_DOC_OUTPUT_PATH) and re-generating. . ."
	# Title and Heading
	echo -e "---\ntitle: Configuration\ndescription: Loki Configuration Examples and Snippets\nweight:  100\n---\n# Configuration" > $(EXAMPLES_DOC_OUTPUT_PATH)
	# Append each configuration and its file name to examples.md
	for f in $$(find $(EXAMPLES_YAML_PATH)/*.yaml -printf "%f\n" | sort -k1n); do \
		echo -e "\n## $$f\n\n\`\`\`yaml\n" >> $(EXAMPLES_DOC_OUTPUT_PATH); \
		grep -v $(EXAMPLES_SKIP_VALIDATION_FLAG) $(EXAMPLES_YAML_PATH)/$$f >> $(EXAMPLES_DOC_OUTPUT_PATH); \
		echo -e "\n\`\`\`\n" >> $(EXAMPLES_DOC_OUTPUT_PATH); \
	done


# Fail our CI build if changes are made to example configurations but our doc is not updated
check-example-config-doc: generate-example-config-doc
	@if ! (git diff --exit-code $(EXAMPLES_DOC_OUTPUT_PATH)); then \
		echo -e "\nChanges found in generated example configuration doc"; \
		echo "Run 'make generate-example-config-doc' and commit the changes to fix this error."; \
		echo "If you are actively developing these files you can ignore this error"; \
		echo -e "(Don't forget to check in the generated files when finished)\n"; \
		exit 1; \
	fi

dev-k3d-loki:
	$(MAKE) -C $(CURDIR)/tools/dev/k3d loki

dev-k3d-enterprise-logs:
	$(MAKE) -C $(CURDIR)/tools/dev/k3d enterprise-logs

dev-k3d-down:
	$(MAKE) -C $(CURDIR)/tools/dev/k3d down

# Trivy is used to scan images for vulnerabilities
.PHONY: trivy
trivy: loki-image build-image
	trivy i $(IMAGE_PREFIX)/loki:$(IMAGE_TAG)
	trivy i $(IMAGE_PREFIX)/loki-build-image:$(IMAGE_TAG)
	trivy fs go.mod

# Synk is also used to scan for vulnerabilities, and detects things that trivy might miss
.PHONY: snyk
snyk: loki-image build-image
	snyk container test $(IMAGE_PREFIX)/loki:$(IMAGE_TAG) --file=cmd/loki/Dockerfile
	snyk container test $(IMAGE_PREFIX)/loki-build-image:$(IMAGE_TAG) --file=loki-build-image/Dockerfile
	snyk container test $(IMAGE_PREFIX)/promtail:$(IMAGE_TAG) --file=clients/cmd/promtail/Dockerfile
	snyk code test

.PHONY: scan-vulnerabilities
scan-vulnerabilities: trivy snyk

.PHONY: release-workflows
release-workflows:
ifeq ($(BUILD_IN_CONTAINER),true)
	$(run_in_container)
else
	pushd $(CURDIR)/.github && jb update && popd
	jsonnet -SJ .github/vendor -m .github/workflows -V BUILD_IMAGE_VERSION=$(BUILD_IMAGE_TAG) -V GO_VERSION=$(GO_VERSION) .github/release-workflows.jsonnet
endif

.PHONY: release-workflows-check
release-workflows-check:
ifeq ($(BUILD_IN_CONTAINER),true)
	$(run_in_container)
else
	@$(MAKE) release-workflows
	@echo "Checking diff"
	@git diff --exit-code --ignore-space-at-eol -- ".github/workflows/*release*" || (echo "Please build release workflows by running 'make release-workflows'" && false)
endif
