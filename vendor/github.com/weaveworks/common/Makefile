.PHONY: all test clean images
.DEFAULT_GOAL := all

# Boiler plate for bulding Docker containers.
# All this must go at top of file I'm afraid.
IMAGE_PREFIX := weaveworks
IMAGE_TAG := $(shell ./tools/image-tag)
UPTODATE := .uptodate
BUILD_IMAGE=quay.io/weaveworks/build-golang:1.10.0-stretch

# Building Docker images is now automated. The convention is every directory
# with a Dockerfile in it builds an image calls quay.io/weaveworks/<dirname>.
# Dependencies (i.e. things that go in the image) still need to be explicitly
# declared.
%/$(UPTODATE): %/Dockerfile
	$(SUDO) docker build -t $(IMAGE_PREFIX)/$(shell basename $(@D)) $(@D)/
	$(SUDO) docker tag $(IMAGE_PREFIX)/$(shell basename $(@D)) $(IMAGE_PREFIX)/$(shell basename $(@D)):$(IMAGE_TAG)
	touch $@

# Get a list of directories containing Dockerfiles
DOCKERFILES := $(shell find . -type f -name Dockerfile ! -path "./tools/*" ! -path "./vendor/*")
UPTODATE_FILES := $(patsubst %/Dockerfile,%/$(UPTODATE),$(DOCKERFILES))
DOCKER_IMAGE_DIRS=$(patsubst %/Dockerfile,%,$(DOCKERFILES))

all: $(UPTODATE_FILES)

# All the boiler plate for building golang follows:
SUDO := $(shell docker info >/dev/null 2>&1 || echo "sudo -E")
BUILD_IN_CONTAINER := true
RM := --rm
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

lint test shell:
	@mkdir -p $(shell pwd)/.pkg
	$(SUDO) docker run $(RM) -ti \
		-v $(shell pwd)/.pkg:/go/pkg \
		-v $(shell pwd):/go/src/github.com/weaveworks/common \
		-e SRC_PATH=/go/src/github.com/weaveworks/common \
		$(BUILD_IMAGE) $@

else

lint:
	./tools/lint -notestpackage -ignorespelling queriers -ignorespelling Queriers .

test:
	go test ./...

shell:
	bash

endif

clean:
	go clean ./...
	rm -rf $(UPTODATE_FILES)
