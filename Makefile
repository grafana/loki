.DEFAULT_GOAL := all
.PHONY: all images check-generated-files logcli loki loki-debug promtail promtail-debug loki-canary lint test clean yacc protos touch-protobuf-sources touch-protos
.PHONY: docker-driver docker-driver-clean docker-driver-enable docker-driver-push
.PHONY: fluent-bit-image, fluent-bit-push, fluent-bit-test
.PHONY: fluentd-image, fluentd-push, fluentd-test
.PHONY: push-images push-latest save-images load-images promtail-image loki-image build-image
.PHONY: bigtable-backup, push-bigtable-backup
.PHONY: benchmark-store, drone, check-mod
.PHONY: migrate migrate-image lint-markdown ragel

SHELL = /usr/bin/env bash

# Empty value = no -mod parameter is used.
# If not empty, GOMOD is passed to -mod= parameter.
# In Go 1.13, "readonly" and "vendor" are accepted.
# In Go 1.14, "readonly", "vendor" and "mod" values are accepted.
# If no value is specified, defaults to "vendor".
#
# Can be used from command line by using "GOMOD= make" (empty = no -mod parameter), or "GOMOD=vendor make" (default).

GOMOD?=vendor
ifeq ($(strip $(GOMOD)),) # Is empty?
	MOD_FLAG=
	GOLANGCI_ARG=
else
	MOD_FLAG=-mod=$(GOMOD)
	GOLANGCI_ARG=--modules-download-mode=$(GOMOD)
endif

#############
# Variables #
#############

DOCKER_IMAGE_DIRS := $(patsubst %/Dockerfile,%,$(DOCKERFILES))

# Certain aspects of the build are done in containers for consistency (e.g. yacc/protobuf generation)
# If you have the correct tools installed and you want to speed up development you can run
# make BUILD_IN_CONTAINER=false target
# or you can override this with an environment variable
BUILD_IN_CONTAINER ?= true
BUILD_IMAGE_VERSION := 0.17.0

# Docker image info
IMAGE_PREFIX ?= grafana

IMAGE_TAG := $(shell ./tools/image-tag)

# Version info for binaries
GIT_REVISION := $(shell git rev-parse --short HEAD)
GIT_BRANCH := $(shell git rev-parse --abbrev-ref HEAD)

# We don't want find to scan inside a bunch of directories, to accelerate the
# 'make: Entering directory '/src/loki' phase.
DONT_FIND := -name tools -prune -o -name vendor -prune -o -name .git -prune -o -name .cache -prune -o -name .pkg -prune -o

# These are all the application files, they are included in the various binary rules as dependencies
# to make sure binaries are rebuilt if any source files change.
APP_GO_FILES := $(shell find . $(DONT_FIND) -name .y.go -prune -o -name .pb.go -prune -o -name cmd -prune -o -type f -name '*.go' -print)

# Build flags
VPREFIX := github.com/grafana/loki/pkg/util/build
GO_LDFLAGS   := -X $(VPREFIX).Branch=$(GIT_BRANCH) -X $(VPREFIX).Version=$(IMAGE_TAG) -X $(VPREFIX).Revision=$(GIT_REVISION) -X $(VPREFIX).BuildUser=$(shell whoami)@$(shell hostname) -X $(VPREFIX).BuildDate=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GO_FLAGS     := -ldflags "-extldflags \"-static\" -s -w $(GO_LDFLAGS)" -tags netgo $(MOD_FLAG)
DYN_GO_FLAGS := -ldflags "-s -w $(GO_LDFLAGS)" -tags netgo $(MOD_FLAG)
# Per some websites I've seen to add `-gcflags "all=-N -l"`, the gcflags seem poorly if at all documented
# the best I could dig up is -N disables optimizations and -l disables inlining which should make debugging match source better.
# Also remove the -s and -w flags present in the normal build which strip the symbol table and the DWARF symbol table.
DEBUG_GO_FLAGS     := -gcflags "all=-N -l" -ldflags "-extldflags \"-static\" $(GO_LDFLAGS)" -tags netgo $(MOD_FLAG)
DYN_DEBUG_GO_FLAGS := -gcflags "all=-N -l" -ldflags "$(GO_LDFLAGS)" -tags netgo $(MOD_FLAG)
# Docker mount flag, ignored on native docker host. see (https://docs.docker.com/docker-for-mac/osxfs-caching/#delegated)
MOUNT_FLAGS := :delegated

NETGO_CHECK = @strings $@ | grep cgo_stub\\\.go >/dev/null || { \
       rm $@; \
       echo "\nYour go standard library was built without the 'netgo' build tag."; \
       echo "To fix that, run"; \
       echo "    sudo go clean -i net"; \
       echo "    sudo go install -tags netgo std"; \
       false; \
}

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

##########
# Docker #
##########

# RM is parameterized to allow CircleCI to run builds, as it
# currently disallows `docker run --rm`. This value is overridden
# in circle.yml
RM := --rm
# TTY is parameterized to allow Google Cloud Builder to run builds,
# as it currently disallows TTY devices. This value needs to be overridden
# in any custom cloudbuild.yaml files
TTY := --tty

DOCKER_BUILDKIT=1
OCI_PLATFORMS=--platform=linux/amd64 --platform=linux/arm64 --platform=linux/arm/7
BUILD_IMAGE = BUILD_IMAGE=$(IMAGE_PREFIX)/loki-build-image:$(BUILD_IMAGE_VERSION)
ifeq ($(CI), true)
	BUILD_OCI=img build --no-console $(OCI_PLATFORMS) --build-arg $(BUILD_IMAGE)
	PUSH_OCI=img push
	TAG_OCI=img tag
else
	BUILD_OCI=docker build --build-arg $(BUILD_IMAGE)
	PUSH_OCI=docker push
	TAG_OCI=docker tag
endif

binfmt:
	$(SUDO) docker run --privileged linuxkit/binfmt:v0.6

################
# Main Targets #
################
all: promtail logcli loki loki-canary check-generated-files

# This is really a check for the CI to make sure generated files are built and checked in manually
check-generated-files: touch-protobuf-sources yacc ragel protos clients/pkg/promtail/server/ui/assets_vfsdata.go
	@if ! (git diff --exit-code $(YACC_GOS) $(RAGEL_GOS) $(PROTO_GOS) $(PROMTAIL_GENERATED_FILE)); then \
		echo "\nChanges found in generated files"; \
		echo "Run 'make check-generated-files' and commit the changes to fix this error."; \
		echo "If you are actively developing these files you can ignore this error"; \
		echo "(Don't forget to check in the generated files when finished)\n"; \
		exit 1; \
	fi

# Trick used to ensure that protobuf files are always compiled even if not changed, because the
# tooling may have been upgraded and the compiled output may be different. We're not using a
# PHONY target so that we can control where we want to touch it.
touch-protobuf-sources:
	for def in $(PROTO_DEFS); do \
		touch $$def; \
	done

##########
# Logcli #
##########

logcli: yacc ragel cmd/logcli/logcli

logcli-image:
	$(SUDO) docker build -t $(IMAGE_PREFIX)/logcli:$(IMAGE_TAG) -f cmd/logcli/Dockerfile .

cmd/logcli/logcli: $(APP_GO_FILES) cmd/logcli/main.go
	CGO_ENABLED=0 go build $(GO_FLAGS) -o $@ ./$(@D)
	$(NETGO_CHECK)

########
# Loki #
########

loki: protos yacc ragel cmd/loki/loki
loki-debug: protos yacc ragel cmd/loki/loki-debug

cmd/loki/loki: $(APP_GO_FILES) cmd/loki/main.go
	CGO_ENABLED=0 go build $(GO_FLAGS) -o $@ ./$(@D)
	$(NETGO_CHECK)

cmd/loki/loki-debug: $(APP_GO_FILES) cmd/loki/main.go
	CGO_ENABLED=0 go build $(DEBUG_GO_FLAGS) -o $@ ./$(@D)
	$(NETGO_CHECK)

###############
# Loki-Canary #
###############

loki-canary: protos yacc ragel cmd/loki-canary/loki-canary

cmd/loki-canary/loki-canary: $(APP_GO_FILES) cmd/loki-canary/main.go
	CGO_ENABLED=0 go build $(GO_FLAGS) -o $@ ./$(@D)
	$(NETGO_CHECK)

#################
# Loki-QueryTee #
#################

loki-querytee: $(APP_GO_FILES) cmd/querytee/main.go
	CGO_ENABLED=0 go build $(GO_FLAGS) -o ./cmd/querytee/$@ ./cmd/querytee/
	$(NETGO_CHECK)

############
# Promtail #
############

PROMTAIL_CGO := 0
PROMTAIL_GO_FLAGS := $(GO_FLAGS)
PROMTAIL_DEBUG_GO_FLAGS := $(DEBUG_GO_FLAGS)

# Validate GOHOSTOS=linux && GOOS=linux to use CGO.
ifeq ($(shell go env GOHOSTOS),linux)
ifeq ($(shell go env GOOS),linux)
ifneq ($(CGO_ENABLED), 0)
PROMTAIL_CGO = 1
endif
PROMTAIL_GO_FLAGS = $(DYN_GO_FLAGS)
PROMTAIL_DEBUG_GO_FLAGS = $(DYN_DEBUG_GO_FLAGS)
endif
endif

promtail: yacc ragel clients/cmd/promtail/promtail
promtail-debug: yacc ragel clients/cmd/promtail/promtail-debug

promtail-clean-assets:
	rm -rf clients/pkg/promtail/server/ui/assets_vfsdata.go

# Rule to generate promtail static assets file
$(PROMTAIL_GENERATED_FILE): $(PROMTAIL_UI_FILES)
	@echo ">> writing assets"
	GOFLAGS="$(MOD_FLAG)" GOOS=$(shell go env GOHOSTOS) go generate -x -v ./clients/pkg/promtail/server/ui

clients/cmd/promtail/promtail: $(APP_GO_FILES) $(PROMTAIL_GENERATED_FILE) clients/cmd/promtail/main.go
	CGO_ENABLED=$(PROMTAIL_CGO) go build $(PROMTAIL_GO_FLAGS) -o $@ ./$(@D)
	$(NETGO_CHECK)

clients/cmd/promtail/promtail-debug: $(APP_GO_FILES) clients/pkg/promtail/server/ui/assets_vfsdata.go clients/cmd/promtail/main.go
	CGO_ENABLED=$(PROMTAIL_CGO) go build $(PROMTAIL_DEBUG_GO_FLAGS) -o $@ ./$(@D)
	$(NETGO_CHECK)

###############
# Migrate #
###############

migrate: cmd/migrate/migrate

cmd/migrate/migrate: $(APP_GO_FILES) cmd/migrate/main.go
	CGO_ENABLED=0 go build $(GO_FLAGS) -o $@ ./$(@D)
	$(NETGO_CHECK)

#############
# Releasing #
#############
# concurrency is limited to 2 to prevent CircleCI from OOMing. Sorry
GOX = gox $(GO_FLAGS) -parallel=2 -output="dist/{{.Dir}}-{{.OS}}-{{.Arch}}"
CGO_GOX = gox $(DYN_GO_FLAGS) -cgo -parallel=2 -output="dist/{{.Dir}}-{{.OS}}-{{.Arch}}"
dist: clean
	CGO_ENABLED=0 $(GOX) -osarch="linux/amd64 linux/arm64 linux/arm darwin/amd64 windows/amd64 freebsd/amd64" ./cmd/loki
	CGO_ENABLED=0 $(GOX) -osarch="linux/amd64 linux/arm64 linux/arm darwin/amd64 windows/amd64 freebsd/amd64" ./cmd/logcli
	CGO_ENABLED=0 $(GOX) -osarch="linux/amd64 linux/arm64 linux/arm darwin/amd64 windows/amd64 freebsd/amd64" ./cmd/loki-canary
	CGO_ENABLED=0 $(GOX) -osarch="linux/arm64 linux/arm darwin/amd64 windows/amd64 windows/386 freebsd/amd64" ./clients/cmd/promtail
	CGO_ENABLED=1 $(CGO_GOX) -osarch="linux/amd64" ./clients/cmd/promtail
	for i in dist/*; do zip -j -m $$i.zip $$i; done
	pushd dist && sha256sum * > SHA256SUMS && popd

publish: dist
	./tools/release

########
# Lint #
########

lint:
	GO111MODULE=on GOGC=10 golangci-lint run -v $(GOLANGCI_ARG)
	faillint -paths "sync/atomic=go.uber.org/atomic" ./...

########
# Test #
########

test: all
	GOGC=10 go test -covermode=atomic -coverprofile=coverage.txt $(MOD_FLAG) -p=4 ./...

#########
# Clean #
#########

clean:
	rm -rf clients/cmd/promtail/promtail
	rm -rf cmd/loki/loki
	rm -rf cmd/logcli/logcli
	rm -rf cmd/loki-canary/loki-canary
	rm -rf cmd/querytee/querytee
	rm -rf .cache
	rm -rf clients/cmd/docker-driver/rootfs
	rm -rf dist/
	rm -rf clients/cmd/fluent-bit/out_grafana_loki.h
	rm -rf clients/cmd/fluent-bit/out_grafana_loki.so
	rm -rf cmd/migrate/migrate
	go clean $(MOD_FLAG) ./...

#########
# YACCs #
#########

yacc: $(YACC_GOS)

%.y.go: %.y
ifeq ($(BUILD_IN_CONTAINER),true)
	# I wish we could make this a multiline variable however you can't pass more than simple arguments to them
	@mkdir -p $(shell pwd)/.pkg
	@mkdir -p $(shell pwd)/.cache
	$(SUDO) docker run $(RM) $(TTY) -i \
		-v $(shell pwd)/.cache:/go/cache$(MOUNT_FLAGS) \
		-v $(shell pwd)/.pkg:/go/pkg$(MOUNT_FLAGS) \
		-v $(shell pwd):/src/loki$(MOUNT_FLAGS) \
		$(IMAGE_PREFIX)/loki-build-image:$(BUILD_IMAGE_VERSION) $@;
else
	goyacc -p $(basename $(notdir $<)) -o $@ $<
	sed -i.back '/^\/\/line/ d' $@
	rm ${@}.back
endif

#########
# Ragels #
#########

ragel: $(RAGEL_GOS)

%.rl.go: %.rl
ifeq ($(BUILD_IN_CONTAINER),true)
	@mkdir -p $(shell pwd)/.pkg
	@mkdir -p $(shell pwd)/.cache
	$(SUDO) docker run $(RM) $(TTY) -i \
		-v $(shell pwd)/.cache:/go/cache$(MOUNT_FLAGS) \
		-v $(shell pwd)/.pkg:/go/pkg$(MOUNT_FLAGS) \
		-v $(shell pwd):/src/loki$(MOUNT_FLAGS) \
		$(IMAGE_PREFIX)/loki-build-image:$(BUILD_IMAGE_VERSION) $@;
else
	ragel -Z $< -o $@
endif

#############
# Protobufs #
#############

protos: $(PROTO_GOS)

# use with care. This signals to make that the proto definitions don't need recompiling.
touch-protos:
	for proto in $(PROTO_GOS); do [ -f "./$${proto}" ] && touch "$${proto}" && echo "touched $${proto}"; done

%.pb.go: $(PROTO_DEFS)
ifeq ($(BUILD_IN_CONTAINER),true)
	@mkdir -p $(shell pwd)/.pkg
	@mkdir -p $(shell pwd)/.cache
	$(SUDO) docker run $(RM) $(TTY) -i \
		-v $(shell pwd)/.cache:/go/cache$(MOUNT_FLAGS) \
		-v $(shell pwd)/.pkg:/go/pkg$(MOUNT_FLAGS) \
		-v $(shell pwd):/src/loki$(MOUNT_FLAGS) \
		$(IMAGE_PREFIX)/loki-build-image:$(BUILD_IMAGE_VERSION) $@;
else
	case "$@" in	\
		vendor*)			\
			protoc -I ./vendor:./$(@D) --gogoslick_out=plugins=grpc:./vendor ./$(patsubst %.pb.go,%.proto,$@); \
			;;					\
		*)						\
			protoc -I .:./vendor:./$(@D) --gogoslick_out=Mgoogle/protobuf/timestamp.proto=github.com/gogo/protobuf/types,plugins=grpc,paths=source_relative:./ ./$(patsubst %.pb.go,%.proto,$@); \
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

docker-driver: docker-driver-clean
	mkdir clients/cmd/docker-driver/rootfs
	docker build -t rootfsimage -f clients/cmd/docker-driver/Dockerfile .
	ID=$$(docker create rootfsimage true) && \
	(docker export $$ID | tar -x -C clients/cmd/docker-driver/rootfs) && \
	docker rm -vf $$ID
	docker rmi rootfsimage -f
	docker plugin create $(LOKI_DOCKER_DRIVER):$(PLUGIN_TAG)$(PLUGIN_ARCH) clients/cmd/docker-driver
	docker plugin create $(LOKI_DOCKER_DRIVER):latest$(PLUGIN_ARCH) clients/cmd/docker-driver

clients/cmd/docker-driver/docker-driver: $(APP_GO_FILES)
	CGO_ENABLED=0 go build $(GO_FLAGS) -o $@ ./$(@D)
	$(NETGO_CHECK)

docker-driver-push: docker-driver
	docker plugin push $(LOKI_DOCKER_DRIVER):$(PLUGIN_TAG)$(PLUGIN_ARCH)
	docker plugin push $(LOKI_DOCKER_DRIVER):latest$(PLUGIN_ARCH)

docker-driver-enable:
	docker plugin enable $(LOKI_DOCKER_DRIVER):$(PLUGIN_TAG)$(PLUGIN_ARCH)

docker-driver-clean:
	-docker plugin disable $(LOKI_DOCKER_DRIVER):$(PLUGIN_TAG)$(PLUGIN_ARCH)
	-docker plugin rm $(LOKI_DOCKER_DRIVER):$(PLUGIN_TAG)$(PLUGIN_ARCH)
	-docker plugin rm $(LOKI_DOCKER_DRIVER):latest$(PLUGIN_ARCH)
	rm -rf clients/cmd/docker-driver/rootfs

#####################
# fluent-bit plugin #
#####################
fluent-bit-plugin:
	go build $(DYN_GO_FLAGS) -buildmode=c-shared -o clients/cmd/fluent-bit/out_grafana_loki.so ./clients/cmd/fluent-bit/

fluent-bit-image:
	$(SUDO) docker build -t $(IMAGE_PREFIX)/fluent-bit-plugin-loki:$(IMAGE_TAG) -f clients/cmd/fluent-bit/Dockerfile .

fluent-bit-push:
	$(SUDO) $(PUSH_OCI) $(IMAGE_PREFIX)/fluent-bit-plugin-loki:$(IMAGE_TAG)

fluent-bit-test: LOKI_URL ?= http://localhost:3100/loki/api/
fluent-bit-test:
	docker run -v /var/log:/var/log -e LOG_PATH="/var/log/*.log" -e LOKI_URL="$(LOKI_URL)" \
	 $(IMAGE_PREFIX)/fluent-bit-plugin-loki:$(IMAGE_TAG)


##################
# fluentd plugin #
##################
fluentd-plugin:
	gem install bundler --version 1.16.2
	bundle config silence_root_warning true
	bundle install --gemfile=clients/cmd/fluentd/Gemfile --path=clients/cmd/fluentd/vendor/bundle

fluentd-image:
	$(SUDO) docker build -t $(IMAGE_PREFIX)/fluent-plugin-loki:$(IMAGE_TAG) -f clients/cmd/fluentd/Dockerfile .

fluentd-push:
	$(SUDO) $(PUSH_OCI) $(IMAGE_PREFIX)/fluent-plugin-loki:$(IMAGE_TAG)

fluentd-test: LOKI_URL ?= http://localhost:3100/loki/api/
fluentd-test:
	LOKI_URL="$(LOKI_URL)" docker-compose -f clients/cmd/fluentd/docker/docker-compose.yml up --build $(IMAGE_PREFIX)/fluent-plugin-loki:$(IMAGE_TAG)

##################
# logstash plugin #
##################
logstash-image:
	$(SUDO) docker build -t $(IMAGE_PREFIX)/logstash-output-loki:$(IMAGE_TAG) -f clients/cmd/logstash/Dockerfile ./

# Send 10 lines to the local Loki instance.
logstash-push-test-logs: LOKI_URL ?= http://host.docker.internal:3100/loki/api/v1/push
logstash-push-test-logs:
	$(SUDO) docker run -e LOKI_URL="$(LOKI_URL)" -v `pwd`/clients/cmd/logstash/loki-test.conf:/home/logstash/loki.conf --rm \
		$(IMAGE_PREFIX)/logstash-output-loki:$(IMAGE_TAG) -f loki.conf

logstash-push:
	$(SUDO) $(PUSH_OCI) $(IMAGE_PREFIX)/logstash-output-loki:$(IMAGE_TAG)

# Enter an env already configure to build and test logstash output plugin.
logstash-env:
	$(SUDO) docker run -v  `pwd`/clients/cmd/logstash:/home/logstash/ -it --rm --entrypoint /bin/sh $(IMAGE_PREFIX)/logstash-output-loki:$(IMAGE_TAG)

########################
# Bigtable Backup Tool #
########################

BIGTABLE_BACKUP_TOOL_FOLDER = ./tools/bigtable-backup
BIGTABLE_BACKUP_TOOL_TAG ?= $(IMAGE_TAG)

bigtable-backup:
	docker build -t $(IMAGE_PREFIX)/$(shell basename $(BIGTABLE_BACKUP_TOOL_FOLDER)) $(BIGTABLE_BACKUP_TOOL_FOLDER)
	docker tag $(IMAGE_PREFIX)/$(shell basename $(BIGTABLE_BACKUP_TOOL_FOLDER)) $(IMAGE_PREFIX)/loki-bigtable-backup:$(BIGTABLE_BACKUP_TOOL_TAG)

push-bigtable-backup: bigtable-backup
	docker push $(IMAGE_PREFIX)/loki-bigtable-backup:$(BIGTABLE_BACKUP_TOOL_TAG)

##########
# Images #
##########

images: promtail-image loki-image loki-canary-image docker-driver fluent-bit-image fluentd-image

# push(app, optional tag)
# pushes the app, optionally tagging it differently before
define push
	$(SUDO) $(TAG_OCI)  $(IMAGE_PREFIX)/$(1):$(IMAGE_TAG) $(IMAGE_PREFIX)/$(1):$(2)
	$(SUDO) $(PUSH_OCI) $(IMAGE_PREFIX)/$(1):$(2)
endef

# push-image(app)
# pushes the app, also as :latest and :master
define push-image
	$(call push,$(1),$(IMAGE_TAG))
	$(call push,$(1),master)
	$(call push,$(1),latest)
endef

# promtail
promtail-image:
	$(SUDO) docker build -t $(IMAGE_PREFIX)/promtail:$(IMAGE_TAG) -f clients/cmd/promtail/Dockerfile .
promtail-image-cross:
	$(SUDO) $(BUILD_OCI) -t $(IMAGE_PREFIX)/promtail:$(IMAGE_TAG) -f clients/cmd/promtail/Dockerfile.cross .

promtail-debug-image: OCI_PLATFORMS=
promtail-debug-image:
	$(SUDO) $(BUILD_OCI) -t $(IMAGE_PREFIX)/promtail:$(IMAGE_TAG)-debug -f clients/cmd/promtail/Dockerfile.debug .

promtail-push: promtail-image-cross
	$(call push-image,promtail)

# loki
loki-image:
	$(SUDO) docker build -t $(IMAGE_PREFIX)/loki:$(IMAGE_TAG) -f cmd/loki/Dockerfile .
loki-image-cross:
	$(SUDO) $(BUILD_OCI) -t $(IMAGE_PREFIX)/loki:$(IMAGE_TAG) -f cmd/loki/Dockerfile.cross .

loki-debug-image: OCI_PLATFORMS=
loki-debug-image:
	$(SUDO) $(BUILD_OCI) -t $(IMAGE_PREFIX)/loki:$(IMAGE_TAG)-debug -f cmd/loki/Dockerfile.debug .

loki-push: loki-image-cross
	$(call push-image,loki)

# loki-canary
loki-canary-image:
	$(SUDO) docker build -t $(IMAGE_PREFIX)/loki-canary:$(IMAGE_TAG) -f cmd/loki-canary/Dockerfile .
loki-canary-image-cross:
	$(SUDO) $(BUILD_OCI) -t $(IMAGE_PREFIX)/loki-canary:$(IMAGE_TAG) -f cmd/loki-canary/Dockerfile.cross .
loki-canary-push: loki-canary-image-cross
	$(SUDO) $(PUSH_OCI) $(IMAGE_PREFIX)/loki-canary:$(IMAGE_TAG)

# loki-querytee
loki-querytee-image:
	$(SUDO) docker build -t $(IMAGE_PREFIX)/loki-querytee:$(IMAGE_TAG) -f cmd/querytee/Dockerfile .
loki-querytee-image-cross:
	$(SUDO) $(BUILD_OCI) -t $(IMAGE_PREFIX)/loki-querytee:$(IMAGE_TAG) -f cmd/querytee/Dockerfile.cross .
loki-querytee-push: loki-querytee-image-cross
	$(SUDO) $(PUSH_OCI) $(IMAGE_PREFIX)/loki-querytee:$(IMAGE_TAG)

# migrate-image
migrate-image:
	$(SUDO) docker build -t $(IMAGE_PREFIX)/loki-migrate:$(IMAGE_TAG) -f cmd/migrate/Dockerfile .


# build-image (only amd64)
build-image: OCI_PLATFORMS=
build-image:
	$(SUDO) $(BUILD_OCI) -t $(IMAGE_PREFIX)/loki-build-image:$(IMAGE_TAG) ./loki-build-image
build-image-push: build-image
ifneq (,$(findstring WIP,$(IMAGE_TAG)))
	@echo "Cannot push a WIP image, commit changes first"; \
	false;
endif
	$(call push,loki-build-image,$(BUILD_IMAGE_VERSION))
	$(call push,loki-build-image,latest)


########
# Misc #
########

benchmark-store:
	go run $(MOD_FLAG) ./pkg/storage/hack/main.go
	go test $(MOD_FLAG) ./pkg/storage/ -bench=.  -benchmem -memprofile memprofile.out -cpuprofile cpuprofile.out -trace trace.out

# regenerate drone yaml
drone:
ifeq ($(BUILD_IN_CONTAINER),true)
	@mkdir -p $(shell pwd)/.pkg
	@mkdir -p $(shell pwd)/.cache
	$(SUDO) docker run $(RM) $(TTY) -i \
		-e DRONE_SERVER -e DRONE_TOKEN \
		-v $(shell pwd)/.cache:/go/cache$(MOUNT_FLAGS) \
		-v $(shell pwd)/.pkg:/go/pkg$(MOUNT_FLAGS) \
		-v $(shell pwd):/src/loki$(MOUNT_FLAGS) \
		$(IMAGE_PREFIX)/loki-build-image:$(BUILD_IMAGE_VERSION) $@;
else
	drone jsonnet --stream --format -V __build-image-version=$(BUILD_IMAGE_VERSION) --source .drone/drone.jsonnet --target .drone/drone.yml
	drone lint .drone/drone.yml
	drone sign --save grafana/loki .drone/drone.yml
endif


# support go modules
check-mod:
ifeq ($(BUILD_IN_CONTAINER),true)
	$(SUDO) docker run  $(RM) $(TTY) -i \
		-v $(shell go env GOPATH)/pkg:/go/pkg$(MOUNT_FLAGS) \
		-v $(shell pwd):/src/loki$(MOUNT_FLAGS) \
		$(IMAGE_PREFIX)/loki-build-image:$(BUILD_IMAGE_VERSION) $@;
else
	GO111MODULE=on GOPROXY=https://proxy.golang.org go mod download
	GO111MODULE=on GOPROXY=https://proxy.golang.org go mod verify
	GO111MODULE=on GOPROXY=https://proxy.golang.org go mod tidy
	GO111MODULE=on GOPROXY=https://proxy.golang.org go mod vendor
endif
	@git diff --exit-code -- go.sum go.mod vendor/


lint-jsonnet:
	@RESULT=0; \
	for f in $$(find . -name 'vendor' -prune -o -name '*.libsonnet' -print -o -name '*.jsonnet' -print); do \
		jsonnetfmt -- "$$f" | diff -u "$$f" -; \
		RESULT=$$(($$RESULT + $$?)); \
	done; \
	exit $$RESULT

fmt-jsonnet:
	@find . -name 'vendor' -prune -o -name '*.libsonnet' -print -o -name '*.jsonnet' -print | \
		xargs -n 1 -- jsonnetfmt -i

lint-scripts:
	@find . -name '*.sh' -not -path '*/vendor/*' -print0 | \
		xargs -0 -n1 shellcheck -x -o all


# search for dead link in our documentation.
# To avoid being rate limited by Github you can use an env variable GITHUB_TOKEN to pass a github token API.
# see https://github.com/settings/tokens
lint-markdown:
ifeq ($(BUILD_IN_CONTAINER),true)
	$(SUDO) docker run $(RM) $(TTY) -i \
		-v $(shell pwd):/src/loki$(MOUNT_FLAGS) \
		$(IMAGE_PREFIX)/loki-build-image:$(BUILD_IMAGE_VERSION) $@;
else
	lychee --verbose --config .lychee.toml ./*.md  ./docs/**/*.md  ./production/**/*.md ./cmd/**/*.md ./clients/**/*.md ./tools/**/*.md
endif


# usage: FUZZ_TESTCASE_PATH=/tmp/testcase make test-fuzz
# this will run the fuzzing using /tmp/testcase and save benchmark locally.
test-fuzz:
	go test -timeout 30s -tags dev,gofuzz -cpuprofile cpu.prof -memprofile mem.prof  \
		-run ^Test_Fuzz$$ github.com/grafana/loki/pkg/logql -v -count=1 -timeout=0s
