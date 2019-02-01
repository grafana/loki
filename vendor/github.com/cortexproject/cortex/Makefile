.PHONY: all test clean images protos
.DEFAULT_GOAL := all

# Boiler plate for bulding Docker containers.
# All this must go at top of file I'm afraid.
IMAGE_PREFIX ?= quay.io/cortexproject/
IMAGE_TAG := $(shell ./tools/image-tag)
GIT_REVISION := $(shell git rev-parse HEAD)
UPTODATE := .uptodate

# Building Docker images is now automated. The convention is every directory
# with a Dockerfile in it builds an image calls quay.io/weaveworks/<dirname>.
# Dependencies (i.e. things that go in the image) still need to be explicitly
# declared.
%/$(UPTODATE): %/Dockerfile
	$(SUDO) docker build --build-arg=revision=$(GIT_REVISION) -t $(IMAGE_PREFIX)$(shell basename $(@D)) $(@D)/
	$(SUDO) docker tag $(IMAGE_PREFIX)$(shell basename $(@D)) $(IMAGE_PREFIX)$(shell basename $(@D)):$(IMAGE_TAG)
	touch $@

# We don't want find to scan inside a bunch of directories, to accelerate the
# 'make: Entering directory '/go/src/github.com/cortexproject/cortex' phase.
DONT_FIND := -name tools -prune -o -name vendor -prune -o -name .git -prune -o -name .cache -prune -o -name .pkg -prune -o

# Get a list of directories containing Dockerfiles
DOCKERFILES := $(shell find . $(DONT_FIND) -type f -name 'Dockerfile' -print)
UPTODATE_FILES := $(patsubst %/Dockerfile,%/$(UPTODATE),$(DOCKERFILES))
DOCKER_IMAGE_DIRS := $(patsubst %/Dockerfile,%,$(DOCKERFILES))
IMAGE_NAMES := $(foreach dir,$(DOCKER_IMAGE_DIRS),$(patsubst %,$(IMAGE_PREFIX)%,$(shell basename $(dir))))
images:
	$(info $(IMAGE_NAMES))
	@echo > /dev/null

# Generating proto code is automated.
PROTO_DEFS := $(shell find . $(DONT_FIND) -type f -name '*.proto' -print)
PROTO_GOS := $(patsubst %.proto,%.pb.go,$(PROTO_DEFS))

# Building binaries is now automated.  The convention is to build a binary
# for every directory with main.go in it, in the ./cmd directory.
MAIN_GO := $(shell find . $(DONT_FIND) -type f -name 'main.go' -print)
EXES := $(foreach exe, $(patsubst ./cmd/%/main.go, %, $(MAIN_GO)), ./cmd/$(exe)/$(exe))
GO_FILES := $(shell find . $(DONT_FIND) -name cmd -prune -o -type f -name '*.go' -print)
define dep_exe
$(1): $(dir $(1))/main.go $(GO_FILES) $(PROTO_GOS)
$(dir $(1))$(UPTODATE): $(1)
endef
$(foreach exe, $(EXES), $(eval $(call dep_exe, $(exe))))

# Manually declared dependancies And what goes into each exe
pkg/ingester/client/cortex.pb.go: pkg/ingester/client/cortex.proto
pkg/ring/ring.pb.go: pkg/ring/ring.proto
pkg/querier/frontend/frontend.pb.go: pkg/querier/frontend/frontend.proto
all: $(UPTODATE_FILES)
test: $(PROTO_GOS)
protos: $(PROTO_GOS)
dep-check: protos
configs-integration-test: $(PROTO_GOS)

# And now what goes into each image
build-image/$(UPTODATE): build-image/*

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
GO_FLAGS := -ldflags "-extldflags \"-static\" -linkmode=external -s -w" -tags netgo -i
NETGO_CHECK = @strings $@ | grep cgo_stub\\\.go >/dev/null || { \
       rm $@; \
       echo "\nYour go standard library was built without the 'netgo' build tag."; \
       echo "To fix that, run"; \
       echo "    sudo go clean -i net"; \
       echo "    sudo go install -tags netgo std"; \
       false; \
}

ifeq ($(BUILD_IN_CONTAINER),true)

$(EXES) $(PROTO_GOS) lint test shell dep-check: build-image/$(UPTODATE)
	@mkdir -p $(shell pwd)/.pkg
	@mkdir -p $(shell pwd)/.cache
	$(SUDO) time docker run $(RM) $(TTY) -i \
		-v $(shell pwd)/.cache:/go/cache \
		-v $(shell pwd)/.pkg:/go/pkg \
		-v $(shell pwd):/go/src/github.com/cortexproject/cortex \
		$(IMAGE_PREFIX)build-image $@;

configs-integration-test: build-image/$(UPTODATE)
	@mkdir -p $(shell pwd)/.pkg
	@mkdir -p $(shell pwd)/.cache
	DB_CONTAINER="$$(docker run -d -e 'POSTGRES_DB=configs_test' postgres:9.6)"; \
	$(SUDO) docker run $(RM) $(TTY) -i \
		-v $(shell pwd)/.cache:/go/cache \
		-v $(shell pwd)/.pkg:/go/pkg \
		-v $(shell pwd):/go/src/github.com/cortexproject/cortex \
		-v $(shell pwd)/cmd/configs/migrations:/migrations \
		-e MIGRATIONS_DIR=/migrations \
		--workdir /go/src/github.com/cortexproject/cortex \
		--link "$$DB_CONTAINER":configs-db.cortex.local \
		$(IMAGE_PREFIX)build-image $@; \
	status=$$?; \
	test -n "$(CIRCLECI)" || docker rm -f "$$DB_CONTAINER"; \
	exit $$status

else

$(EXES):
	go build $(GO_FLAGS) -o $@ ./$(@D)
	$(NETGO_CHECK)

%.pb.go:
	protoc -I $(GOPATH)/src:./vendor:./$(@D) --gogoslick_out=plugins=grpc:./$(@D) ./$(patsubst %.pb.go,%.proto,$@)

lint:
	./tools/lint -notestpackage -ignorespelling queriers -ignorespelling Queriers .

test:
	./tools/test -netgo

shell:
	bash

configs-integration-test:
	/bin/bash -c "go test -tags 'netgo integration' -timeout 30s ./pkg/configs/... ./pkg/ruler/..."

dep-check:
	dep check

endif

clean:
	$(SUDO) docker rmi $(IMAGE_NAMES) >/dev/null 2>&1 || true
	rm -rf $(UPTODATE_FILES) $(EXES) $(PROTO_GOS) .cache
	go clean ./...

save-images:
	@mkdir -p images
	for image_name in $(IMAGE_NAMES); do \
		if ! echo $$image_name | grep build; then \
			docker save $$image_name:$(IMAGE_TAG) -o images/$$(echo $$image_name | tr "/" _):$(IMAGE_TAG); \
		fi \
	done

load-images:
	for image_name in $(IMAGE_NAMES); do \
		if ! echo $$image_name | grep build; then \
			docker load -i images/$$(echo $$image_name | tr "/" _):$(IMAGE_TAG); \
		fi \
	done
