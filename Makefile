.DEFAULT_GOAL := all
.PHONY: all images check-generated-files logcli loki loki-debug promtail promtail-debug loki-canary lint test clean yacc protos touch-protobuf-sources
.PHONY: helm helm-install helm-upgrade helm-publish helm-debug helm-clean
.PHONY: docker-driver docker-driver-clean docker-driver-enable docker-driver-push
.PHONY: fluent-bit-image, fluent-bit-push, fluent-bit-test
.PHONY: push-images push-latest save-images load-images promtail-image loki-image build-image
.PHONY: bigtable-backup, push-bigtable-backup
.PHONY: benchmark-store, drone, check-mod

SHELL = /usr/bin/env bash
#############
# Variables #
#############

DOCKER_IMAGE_DIRS := $(patsubst %/Dockerfile,%,$(DOCKERFILES))
IMAGE_NAMES := $(foreach dir,$(DOCKER_IMAGE_DIRS),$(patsubst %,$(IMAGE_PREFIX)%,$(shell basename $(dir))))

# Certain aspects of the build are done in containers for consistency (e.g. yacc/protobuf generation)
# If you have the correct tools installed and you want to speed up development you can run
# make BUILD_IN_CONTAINER=false target
# or you can override this with an environment variable
BUILD_IN_CONTAINER ?= true
BUILD_IMAGE_VERSION := 0.7.4

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
VPREFIX := github.com/grafana/loki/vendor/github.com/prometheus/common/version
GO_LDFLAGS   := -s -w -X $(VPREFIX).Branch=$(GIT_BRANCH) -X $(VPREFIX).Version=$(IMAGE_TAG) -X $(VPREFIX).Revision=$(GIT_REVISION) -X $(VPREFIX).BuildUser=$(shell whoami)@$(shell hostname) -X $(VPREFIX).BuildDate=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GO_FLAGS     := -ldflags "-extldflags \"-static\" $(GO_LDFLAGS)" -tags netgo -mod vendor
DYN_GO_FLAGS := -ldflags "$(GO_LDFLAGS)" -tags netgo -mod vendor
# Per some websites I've seen to add `-gcflags "all=-N -l"`, the gcflags seem poorly if at all documented
# the best I could dig up is -N disables optimizations and -l disables inlining which should make debugging match source better.
# Also remove the -s and -w flags present in the normal build which strip the symbol table and the DWARF symbol table.
DEBUG_GO_FLAGS     := -gcflags "all=-N -l" -ldflags "-extldflags \"-static\" $(GO_LDFLAGS)" -tags netgo -mod vendor
DYN_DEBUG_GO_FLAGS := -gcflags "all=-N -l" -ldflags "$(GO_LDFLAGS)" -tags netgo -mod vendor

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

# Promtail UI files
PROMTAIL_GENERATED_FILE := pkg/promtail/server/ui/assets_vfsdata.go
PROMTAIL_UI_FILES := $(shell find ./pkg/promtail/server/ui -type f -name assets_vfsdata.go -prune -o -print)

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
check-generated-files: touch-protobuf-sources yacc protos pkg/promtail/server/ui/assets_vfsdata.go
	@if ! (git diff --exit-code $(YACC_GOS) $(PROTO_GOS) $(PROMTAIL_GENERATED_FILE)); then \
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

logcli: yacc cmd/logcli/logcli

cmd/logcli/logcli: $(APP_GO_FILES) cmd/logcli/main.go
	CGO_ENABLED=0 go build $(GO_FLAGS) -o $@ ./$(@D)
	$(NETGO_CHECK)

########
# Loki #
########

loki: protos yacc cmd/loki/loki
loki-debug: protos yacc cmd/loki/loki-debug

cmd/loki/loki: $(APP_GO_FILES) cmd/loki/main.go
	CGO_ENABLED=0 go build $(GO_FLAGS) -o $@ ./$(@D)
	$(NETGO_CHECK)

cmd/loki/loki-debug: $(APP_GO_FILES) cmd/loki/main.go
	CGO_ENABLED=0 go build $(DEBUG_GO_FLAGS) -o $@ ./$(@D)
	$(NETGO_CHECK)

###############
# Loki-Canary #
###############

loki-canary: protos yacc cmd/loki-canary/loki-canary

cmd/loki-canary/loki-canary: $(APP_GO_FILES) cmd/loki-canary/main.go
	CGO_ENABLED=0 go build $(GO_FLAGS) -o $@ ./$(@D)
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
ifeq ($(shell go env GOARCH),amd64)
PROMTAIL_CGO = 1
PROMTAIL_GO_FLAGS = $(DYN_GO_FLAGS)
PROMTAIL_DEBUG_GO_FLAGS = $(DYN_DEBUG_GO_FLAGS)
endif
endif
endif

promtail: yacc cmd/promtail/promtail
promtail-debug: yacc cmd/promtail/promtail-debug

promtail-clean-assets:
	rm -rf pkg/promtail/server/ui/assets_vfsdata.go

# Rule to generate promtail static assets file
$(PROMTAIL_GENERATED_FILE): $(PROMTAIL_UI_FILES)
	@echo ">> writing assets"
	GOFLAGS=-mod=vendor GOOS=$(shell go env GOHOSTOS) go generate -x -v ./pkg/promtail/server/ui

cmd/promtail/promtail: $(APP_GO_FILES) $(PROMTAIL_GENERATED_FILE) cmd/promtail/main.go
	CGO_ENABLED=$(PROMTAIL_CGO) go build $(PROMTAIL_GO_FLAGS) -o $@ ./$(@D)
	$(NETGO_CHECK)

cmd/promtail/promtail-debug: $(APP_GO_FILES) pkg/promtail/server/ui/assets_vfsdata.go cmd/promtail/main.go
	CGO_ENABLED=$(PROMTAIL_CGO) go build $(PROMTAIL_DEBUG_GO_FLAGS) -o $@ ./$(@D)
	$(NETGO_CHECK)

#############
# Releasing #
#############
# concurrency is limited to 2 to prevent CircleCI from OOMing. Sorry
GOX = gox $(GO_FLAGS) -parallel=2 -output="dist/{{.Dir}}-{{.OS}}-{{.Arch}}"
CGO_GOX = gox $(DYN_GO_FLAGS) -cgo -parallel=2 -output="dist/{{.Dir}}-{{.OS}}-{{.Arch}}"
dist: clean
	CGO_ENABLED=0 $(GOX) -osarch="linux/amd64 linux/arm64 linux/arm windows/amd64" ./cmd/loki
	CGO_ENABLED=0 $(GOX) -osarch="linux/amd64 linux/arm64 linux/arm darwin/amd64 windows/amd64 freebsd/amd64" ./cmd/logcli
	CGO_ENABLED=0 $(GOX) -osarch="linux/amd64 linux/arm64 linux/arm darwin/amd64 windows/amd64 freebsd/amd64" ./cmd/loki-canary
	CGO_ENABLED=0 $(GOX) -osarch="linux/arm64 linux/arm darwin/amd64 windows/amd64 freebsd/amd64" ./cmd/promtail
	CGO_ENABLED=1 $(CGO_GOX) -osarch="linux/amd64" ./cmd/promtail
	gzip dist/*
	pushd dist && sha256sum * > SHA256SUMS && popd

publish: dist
	./tools/release

########
# Lint #
########

lint:
	GO111MODULE=on GOGC=10 golangci-lint run

########
# Test #
########

test: all
	GOGC=10 go test -mod=vendor -p=4 ./...

#########
# Clean #
#########

clean:
	rm -rf cmd/promtail/promtail
	rm -rf cmd/loki/loki
	rm -rf cmd/logcli/logcli
	rm -rf cmd/loki-canary/loki-canary
	rm -rf .cache
	rm -rf cmd/docker-driver/rootfs
	rm -rf dist/
	rm -rf cmd/fluent-bit/out_loki.h
	rm -rf cmd/fluent-bit/out_loki.so
	go clean -mod=vendor ./...

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
		-v $(shell pwd)/.cache:/go/cache \
		-v $(shell pwd)/.pkg:/go/pkg \
		-v $(shell pwd):/src/loki \
		$(IMAGE_PREFIX)/loki-build-image:$(BUILD_IMAGE_VERSION) $@;
else
	goyacc -p $(basename $(notdir $<)) -o $@ $<
endif

#############
# Protobufs #
#############

protos: $(PROTO_GOS)

%.pb.go: $(PROTO_DEFS)
ifeq ($(BUILD_IN_CONTAINER),true)
	@mkdir -p $(shell pwd)/.pkg
	@mkdir -p $(shell pwd)/.cache
	$(SUDO) docker run $(RM) $(TTY) -i \
		-v $(shell pwd)/.cache:/go/cache \
		-v $(shell pwd)/.pkg:/go/pkg \
		-v $(shell pwd):/src/loki \
		$(IMAGE_PREFIX)/loki-build-image:$(BUILD_IMAGE_VERSION) $@;
else
	case "$@" in	\
		vendor*)			\
			protoc -I ./vendor:./$(@D) --gogoslick_out=plugins=grpc:./vendor ./$(patsubst %.pb.go,%.proto,$@); \
			;;					\
		*)						\
			protoc -I ./vendor:./$(@D) --gogoslick_out=Mgoogle/protobuf/timestamp.proto=github.com/gogo/protobuf/types,plugins=grpc:./$(@D) ./$(patsubst %.pb.go,%.proto,$@); \
			;;					\
		esac
endif


########
# Helm #
########

CHARTS := production/helm/loki production/helm/promtail production/helm/fluent-bit production/helm/loki-stack

helm:
	-rm -f production/helm/*/requirements.lock
	@set -e; \
	helm init -c; \
	for chart in $(CHARTS); do \
		helm dependency build $$chart; \
		helm lint $$chart; \
		helm package $$chart; \
	done
	rm -f production/helm/*/requirements.lock

helm-install:
	kubectl apply -f tools/helm.yaml
	helm init --wait --service-account helm --upgrade
	HELM_ARGS="$(HELM_ARGS)" $(MAKE) helm-upgrade

helm-install-fluent-bit:
	HELM_ARGS="--set fluent-bit.enabled=true,promtail.enabled=false" $(MAKE) helm-install


helm-upgrade: helm
	helm upgrade --wait --install $(ARGS) loki-stack ./production/helm/loki-stack \
	--set promtail.image.tag=$(IMAGE_TAG) --set loki.image.tag=$(IMAGE_TAG) --set fluent-bit.image.tag=$(IMAGE_TAG) -f tools/dev.values.yaml $(HELM_ARGS)

helm-publish: helm
	cp production/helm/README.md index.md
	git config user.email "$CIRCLE_USERNAME@users.noreply.github.com"
	git config user.name "${CIRCLE_USERNAME}"
	git checkout gh-pages || (git checkout --orphan gh-pages && git rm -rf . > /dev/null)
	mkdir -p charts
	mv *.tgz index.md charts/
	helm repo index charts/
	git add charts/
	git commit -m "[skip ci] Publishing helm charts: ${CIRCLE_SHA1}"
	git push origin gh-pages

helm-debug: ARGS=--dry-run --debug
helm-debug: helm-upgrade

helm-clean:
	-helm delete --purge loki-stack

#################
# Docker Driver #
#################

# optionally set the tag or the arch suffix (-arm64)
PLUGIN_TAG ?= $(IMAGE_TAG)
PLUGIN_ARCH ?=

docker-driver: docker-driver-clean
	mkdir cmd/docker-driver/rootfs
	docker build -t rootfsimage -f cmd/docker-driver/Dockerfile .
	ID=$$(docker create rootfsimage true) && \
	(docker export $$ID | tar -x -C cmd/docker-driver/rootfs) && \
	docker rm -vf $$ID
	docker rmi rootfsimage -f
	docker plugin create grafana/loki-docker-driver:$(PLUGIN_TAG)$(PLUGIN_ARCH) cmd/docker-driver
	docker plugin create grafana/loki-docker-driver:latest$(PLUGIN_ARCH) cmd/docker-driver

cmd/docker-driver/docker-driver: $(APP_GO_FILES)
	CGO_ENABLED=0 go build $(GO_FLAGS) -o $@ ./$(@D)
	$(NETGO_CHECK)

docker-driver-push: docker-driver
	docker plugin push grafana/loki-docker-driver:$(PLUGIN_TAG)$(PLUGIN_ARCH)
	docker plugin push grafana/loki-docker-driver:latest$(PLUGIN_ARCH)

docker-driver-enable:
	docker plugin enable grafana/loki-docker-driver:$(PLUGIN_TAG)$(PLUGIN_ARCH)

docker-driver-clean:
	-docker plugin disable grafana/loki-docker-driver:$(PLUGIN_TAG)$(PLUGIN_ARCH)
	-docker plugin rm grafana/loki-docker-driver:$(PLUGIN_TAG)$(PLUGIN_ARCH)
	-docker plugin rm grafana/loki-docker-driver:latest$(PLUGIN_ARCH)
	rm -rf cmd/docker-driver/rootfs

#####################
# fluent-bit plugin #
#####################
fluent-bit-plugin:
	go build $(DYN_GO_FLAGS) -buildmode=c-shared -o cmd/fluent-bit/out_loki.so ./cmd/fluent-bit/

fluent-bit-image:
	$(SUDO) docker build -t $(IMAGE_PREFIX)/fluent-bit-plugin-loki:$(IMAGE_TAG) -f cmd/fluent-bit/Dockerfile .

fluent-bit-push:
	$(SUDO) $(PUSH_OCI) $(IMAGE_PREFIX)/fluent-bit-plugin-loki:$(IMAGE_TAG)

fluent-bit-test: LOKI_URL ?= http://localhost:3100/loki/api/
fluent-bit-test:
	docker run -v /var/log:/var/log -e LOG_PATH="/var/log/*.log" -e LOKI_URL="$(LOKI_URL)" \
	 $(IMAGE_PREFIX)/fluent-bit-plugin-loki:$(IMAGE_TAG)

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

images: promtail-image loki-image loki-canary-image docker-driver fluent-bit-image

print-images:
	$(info $(patsubst %,%:$(IMAGE_TAG),$(IMAGE_NAMES)))
	@echo > /dev/null

IMAGE_NAMES := grafana/loki grafana/promtail grafana/loki-canary

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
	$(SUDO) docker build -t $(IMAGE_PREFIX)/promtail:$(IMAGE_TAG) -f cmd/promtail/Dockerfile .
promtail-image-cross:
	$(SUDO) $(BUILD_OCI) -t $(IMAGE_PREFIX)/promtail:$(IMAGE_TAG) -f cmd/promtail/Dockerfile.cross .

promtail-debug-image: OCI_PLATFORMS=
promtail-debug-image:
	$(SUDO) $(BUILD_OCI) -t $(IMAGE_PREFIX)/promtail:$(IMAGE_TAG)-debug -f cmd/promtail/Dockerfile.debug .

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

# build-image (only amd64)
build-image: OCI_PLATFORMS=
build-image:
	$(SUDO) $(BUILD_OCI) -t $(IMAGE_PREFIX)/loki-build-image:$(IMAGE_TAG) ./loki-build-image
build-image-push: build-image
	$(call push,loki-build-image,$(BUILD_IMAGE_VERSION))
	$(call push,loki-build-image,latest)


########
# Misc #
########

benchmark-store:
	go run -mod=vendor ./pkg/storage/hack/main.go
	go test -mod=vendor ./pkg/storage/ -bench=.  -benchmem -memprofile memprofile.out -cpuprofile cpuprofile.out

# regenerate drone yaml
drone:
	jsonnet -V __build-image-version=$(BUILD_IMAGE_VERSION) .drone/drone.jsonnet | jq .drone -r | yq -y . > .drone/drone.yml

# support go modules
check-mod:
ifeq ($(BUILD_IN_CONTAINER),true)
	$(SUDO) docker run  $(RM) $(TTY) -i \
		-v $(shell go env GOPATH)/pkg:/go/pkg \
		-v $(shell pwd):/src/loki \
		$(IMAGE_PREFIX)/loki-build-image:$(BUILD_IMAGE_VERSION) $@;
else
	GO111MODULE=on GOPROXY=https://proxy.golang.org go mod download
	GO111MODULE=on GOPROXY=https://proxy.golang.org go mod verify
	GO111MODULE=on GOPROXY=https://proxy.golang.org go mod tidy
	GO111MODULE=on GOPROXY=https://proxy.golang.org go mod vendor
endif
	@git diff --exit-code -- go.sum go.mod vendor/
