.PHONY: all test clean images protos
.DEFAULT_GOAL := all

# Boiler plate for bulding Docker containers.
# All this must go at top of file I'm afraid.
IMAGE_PREFIX ?= grafana/
IMAGE_TAG := $(shell ./tools/image-tag)
UPTODATE := .uptodate

# Building Docker images is now automated. The convention is every directory
# with a Dockerfile in it builds an image calls quay.io/grafana/loki-<dirname>.
# Dependencies (i.e. things that go in the image) still need to be explicitly
# declared.
%/$(UPTODATE): %/Dockerfile
	$(SUDO) docker build -t $(IMAGE_PREFIX)$(shell basename $(@D)) $(@D)/
	$(SUDO) docker tag $(IMAGE_PREFIX)$(shell basename $(@D)) $(IMAGE_PREFIX)$(shell basename $(@D)):$(IMAGE_TAG)
	touch $@

# We don't want find to scan inside a bunch of directories, to accelerate the
# 'make: Entering directory '/go/src/github.com/grafana/loki' phase.
DONT_FIND := -name tools -prune -o -name vendor -prune -o -name .git -prune -o -name .cache -prune -o -name .pkg -prune -o

# Get a list of directories containing Dockerfiles
DOCKERFILES := $(shell find . $(DONT_FIND) -type f -name 'Dockerfile' -print)
UPTODATE_FILES := $(patsubst %/Dockerfile,%/$(UPTODATE),$(DOCKERFILES))
DOCKER_IMAGE_DIRS := $(patsubst %/Dockerfile,%,$(DOCKERFILES))
IMAGE_NAMES := $(foreach dir,$(DOCKER_IMAGE_DIRS),$(patsubst %,$(IMAGE_PREFIX)%,$(shell basename $(dir))))
images:
	$(info $(patsubst %,%:$(IMAGE_TAG),$(IMAGE_NAMES)))
	@echo > /dev/null

# Generating proto code is automated.
PROTO_DEFS := $(shell find . $(DONT_FIND) -type f -name '*.proto' -print)
PROTO_GOS := $(patsubst %.proto,%.pb.go,$(PROTO_DEFS)) \
	vendor/github.com/cortexproject/cortex/pkg/ring/ring.pb.go \
	vendor/github.com/cortexproject/cortex/pkg/ingester/client/cortex.pb.go \
	vendor/github.com/cortexproject/cortex/pkg/chunk/storage/caching_index_client.pb.go

# Generating yacc code is automated.
YACC_DEFS := $(shell find . $(DONT_FIND) -type f -name *.y -print)
YACC_GOS := $(patsubst %.y,%.go,$(YACC_DEFS))

# Building binaries is now automated.  The convention is to build a binary
# for every directory with main.go in it, in the ./cmd directory.
MAIN_GO := $(shell find . $(DONT_FIND) -type f -name 'main.go' -print)
EXES := $(foreach exe, $(patsubst ./cmd/%/main.go, %, $(MAIN_GO)), ./cmd/$(exe)/$(exe))
GO_FILES := $(shell find . $(DONT_FIND) -name cmd -prune -o -type f -name '*.go' -print)
define dep_exe
$(1): $(dir $(1))/main.go $(GO_FILES) $(PROTO_GOS) $(YACC_GOS)
$(dir $(1))$(UPTODATE): $(1)
endef
$(foreach exe, $(EXES), $(eval $(call dep_exe, $(exe))))

# Manually declared dependancies and what goes into each exe
pkg/logproto/logproto.pb.go: pkg/logproto/logproto.proto
vendor/github.com/cortexproject/cortex/pkg/ring/ring.pb.go: vendor/github.com/cortexproject/cortex/pkg/ring/ring.proto
vendor/github.com/cortexproject/cortex/pkg/ingester/client/cortex.pb.go: vendor/github.com/cortexproject/cortex/pkg/ingester/client/cortex.proto
vendor/github.com/cortexproject/cortex/pkg/chunk/storage/caching_index_client.pb.go: vendor/github.com/cortexproject/cortex/pkg/chunk/storage/caching_index_client.proto
pkg/parser/labels.go: pkg/parser/labels.y
pkg/parser/matchers.go: pkg/parser/matchers.y
all: $(UPTODATE_FILES)
test: $(PROTO_GOS) $(YACC_GOS)
yacc: $(YACC_GOS)
protos: $(PROTO_GOS)
yacc: $(YACC_GOS)

# And now what goes into each image
loki-build-image/$(UPTODATE): loki-build-image/*

# All the boiler plate for building golang follows:
SUDO := $(shell docker info >/dev/null 2>&1 || echo "sudo -E")
BUILD_IN_CONTAINER := true
# RM is parameterized to allow CircleCI to run builds, as it
# currently disallows `docker run --rm`. This value is overridden
# in circle.yml
RM := --rm
# TTY is parameterized to allow Google Cloud Builder to run builds,
# as it currently disallows TTY devices. This value needs to be overridden
# in any custom cloudbuild.yaml files
TTY := --tty
GO_FLAGS := -ldflags "-extldflags \"-static\" -s -w" -tags netgo
NETGO_CHECK = @strings $@ | grep cgo_stub\\\.go >/dev/null || { \
       rm $@; \
       echo "\nYour go standard library was built without the 'netgo' build tag."; \
       echo "To fix that, run"; \
       echo "    sudo go clean -i net"; \
       echo "    sudo go install -tags netgo std"; \
       false; \
}

ifeq ($(BUILD_IN_CONTAINER),true)

$(EXES) $(PROTO_GOS) $(YACC_GOS) lint test shell check-generated-files: loki-build-image/$(UPTODATE)
	@mkdir -p $(shell pwd)/.pkg
	@mkdir -p $(shell pwd)/.cache
	$(SUDO) docker run $(RM) $(TTY) -i \
		-v $(shell pwd)/.cache:/go/cache \
		-v $(shell pwd)/.pkg:/go/pkg \
		-v $(shell pwd):/go/src/github.com/grafana/loki \
		$(IMAGE_PREFIX)loki-build-image $@;

else

$(EXES): loki-build-image/$(UPTODATE)
	CGO_ENABLED=0 go build $(GO_FLAGS) -o $@ ./$(@D)
	$(NETGO_CHECK)

%.pb.go: loki-build-image/$(UPTODATE)
	case "$@" in 	\
	vendor*)			\
		protoc -I ./vendor:./$(@D) --gogoslick_out=plugins=grpc:./vendor ./$(patsubst %.pb.go,%.proto,$@); \
		;;					\
	*)						\
		protoc -I ./vendor:./$(@D) --gogoslick_out=Mgoogle/protobuf/timestamp.proto=github.com/gogo/protobuf/types,plugins=grpc:./$(@D) ./$(patsubst %.pb.go,%.proto,$@); \
		;;					\
	esac

%.go: %.y
	goyacc -p $(basename $(notdir $<)) -o $@ $<

lint: loki-build-image/$(UPTODATE)
	gometalinter ./...

check-generated-files: loki-build-image/$(UPTODATE) yacc protos
	@git diff-files || (echo "changed files; failing check" && exit 1)

test: loki-build-image/$(UPTODATE)
	go test ./...

shell: loki-build-image/$(UPTODATE)
	bash

endif

save-images:
	@set -e; \
	mkdir -p images; \
	for image_name in $(IMAGE_NAMES); do \
		if ! echo $$image_name | grep build; then \
			docker save $$image_name:$(IMAGE_TAG) -o images/$$(echo $$image_name | tr "/" _):$(IMAGE_TAG); \
		fi \
	done

load-images:
	@set -e; \
	mkdir -p images; \
	for image_name in $(IMAGE_NAMES); do \
		if ! echo $$image_name | grep build; then \
			docker load -i images/$$(echo $$image_name | tr "/" _):$(IMAGE_TAG); \
		fi \
	done

push-images:
	@set -e; \
	for image_name in $(IMAGE_NAMES); do \
		if ! echo $$image_name | grep build; then \
			docker push $$image_name:$(IMAGE_TAG); \
		fi \
	done

push-master:
	@set -e; \
	for image_name in $(IMAGE_NAMES); do \
		if ! echo $$image_name | grep build; then \
			docker tag $$image_name:$(IMAGE_TAG) $$image_name:master; \
			docker push $$image_name:master; \
		fi \
	done

clean:
	$(SUDO) docker rmi $(IMAGE_NAMES) >/dev/null 2>&1 || true
	rm -rf $(UPTODATE_FILES) $(EXES) .cache
	go clean ./...
