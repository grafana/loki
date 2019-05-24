.PHONY: all test clean images protos assets check_assets
.DEFAULT_GOAL := all

CHARTS := production/helm/loki production/helm/promtail production/helm/loki-stack

# Boiler plate for bulding Docker containers.
# All this must go at top of file I'm afraid.
IMAGE_PREFIX ?= grafana/
IMAGE_TAG := $(shell ./tools/image-tag)
UPTODATE := .uptodate
DEBUG_UPTODATE := .uptodate-debug
GIT_REVISION := $(shell git rev-parse --short HEAD)
GIT_BRANCH := $(shell git rev-parse --abbrev-ref HEAD)


# Building Docker images is now automated. The convention is every directory
# with a Dockerfile in it builds an image calls quay.io/grafana/loki-<dirname>.
# Dependencies (i.e. things that go in the image) still need to be explicitly
# declared.
%/$(UPTODATE): %/Dockerfile
	$(SUDO) docker build -t $(IMAGE_PREFIX)$(shell basename $(@D)) $(@D)/
	$(SUDO) docker tag $(IMAGE_PREFIX)$(shell basename $(@D)) $(IMAGE_PREFIX)$(shell basename $(@D)):$(IMAGE_TAG)
	touch $@

%/$(DEBUG_UPTODATE): %/Dockerfile.debug
	$(SUDO) docker build -f $(@D)/Dockerfile.debug -t $(IMAGE_PREFIX)$(shell basename $(@D))-debug $(@D)/
	$(SUDO) docker tag $(IMAGE_PREFIX)$(shell basename $(@D))-debug $(IMAGE_PREFIX)$(shell basename $(@D))-debug:$(IMAGE_TAG)
	touch $@

# We don't want find to scan inside a bunch of directories, to accelerate the
# 'make: Entering directory '/go/src/github.com/grafana/loki' phase.
DONT_FIND := -name tools -prune -o -name vendor -prune -o -name .git -prune -o -name .cache -prune -o -name .pkg -prune -o

# Get a list of directories containing Dockerfiles
DOCKERFILES := $(shell find . $(DONT_FIND) -type f -name 'Dockerfile' -print)
UPTODATE_FILES := $(patsubst %/Dockerfile,%/$(UPTODATE),$(DOCKERFILES))
DEBUG_DOCKERFILES := $(shell find . $(DONT_FIND) -type f -name 'Dockerfile.debug' -print)
DEBUG_UPTODATE_FILES := $(patsubst %/Dockerfile.debug,%/$(DEBUG_UPTODATE),$(DEBUG_DOCKERFILES))
DEBUG_DLV_FILES := $(patsubst %/Dockerfile.debug,%/dlv,$(DEBUG_DOCKERFILES))
DOCKER_IMAGE_DIRS := $(patsubst %/Dockerfile,%,$(DOCKERFILES))
IMAGE_NAMES := $(foreach dir,$(DOCKER_IMAGE_DIRS),$(patsubst %,$(IMAGE_PREFIX)%,$(shell basename $(dir))))
DEBUG_DOCKER_IMAGE_DIRS := $(patsubst %/Dockerfile.debug,%,$(DEBUG_DOCKERFILES))
DEBUG_IMAGE_NAMES := $(foreach dir,$(DEBUG_DOCKER_IMAGE_DIRS),$(patsubst %,$(IMAGE_PREFIX)%,$(shell basename $(dir))-debug))
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
DEBUG_EXES := $(foreach exe, $(patsubst ./cmd/%/main.go, %, $(MAIN_GO)), ./cmd/$(exe)/$(exe)-debug)
GO_FILES := $(shell find . $(DONT_FIND) -name cmd -prune -o -type f -name '*.go' -print)

# This is the important part of how `make all` enters this file
# the above EXES finds all the main.go files and for each of them
# it creates the dep_exe targets which look like this:
#   cmd/promtail/promtail: loki-build-image/.uptodate cmd/promtail//main.go pkg/loki/loki.go pkg/loki/fake_auth.go ...
#   cmd/promtail/.uptodate: cmd/promtail/promtail
# Then when `make all` expands `$(UPTODATE_FILES)` it will call the second generated target and initiate the build process
define dep_exe
$(1): $(dir $(1))/main.go $(GO_FILES) $(PROTO_GOS) $(YACC_GOS)
$(dir $(1))$(UPTODATE): $(1)
endef
$(foreach exe, $(EXES), $(eval $(call dep_exe, $(exe))))

# Everything is basically duplicated for debug builds,
# but with a different set of Dockerfiles and binaries appended with -debug.
define debug_dep_exe
$(1): $(dir $(1))/main.go $(GO_FILES) $(PROTO_GOS) $(YACC_GOS)
$(dir $(1))$(DEBUG_UPTODATE): $(1)
endef
$(foreach exe, $(DEBUG_EXES), $(eval $(call debug_dep_exe, $(exe))))

# Manually declared dependancies and what goes into each exe
pkg/logproto/logproto.pb.go: pkg/logproto/logproto.proto
vendor/github.com/cortexproject/cortex/pkg/ring/ring.pb.go: vendor/github.com/cortexproject/cortex/pkg/ring/ring.proto
vendor/github.com/cortexproject/cortex/pkg/ingester/client/cortex.pb.go: vendor/github.com/cortexproject/cortex/pkg/ingester/client/cortex.proto
vendor/github.com/cortexproject/cortex/pkg/chunk/storage/caching_index_client.pb.go: vendor/github.com/cortexproject/cortex/pkg/chunk/storage/caching_index_client.proto
pkg/promtail/server/server.go: assets
pkg/logql/expr.go: pkg/logql/expr.y
all: $(UPTODATE_FILES)
test: $(PROTO_GOS) $(YACC_GOS)
debug: $(DEBUG_UPTODATE_FILES)
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

VPREFIX := github.com/grafana/loki/vendor/github.com/prometheus/common/version
GO_FLAGS := -ldflags "-extldflags \"-static\" -s -w -X $(VPREFIX).Branch=$(GIT_BRANCH) -X $(VPREFIX).Version=$(IMAGE_TAG) -X $(VPREFIX).Revision=$(GIT_REVISION)" -tags netgo
# Per some websites I've seen to add `-gcflags "all=-N -l"`, the gcflags seem poorly if at all documented
# the best I could dig up is -N disables optimizations and -l disables inlining which should make debugging match source better.
# Also remove the -s and -w flags present in the normal build which strip the symbol table and the DWARF symbol table.
DEBUG_GO_FLAGS := -gcflags "all=-N -l" -ldflags "-extldflags \"-static\" -X $(VPREFIX).Branch=$(GIT_BRANCH) -X $(VPREFIX).Version=$(IMAGE_TAG) -X $(VPREFIX).Revision=$(GIT_REVISION)" -tags netgo

NETGO_CHECK = @strings $@ | grep cgo_stub\\\.go >/dev/null || { \
       rm $@; \
       echo "\nYour go standard library was built without the 'netgo' build tag."; \
       echo "To fix that, run"; \
       echo "    sudo go clean -i net"; \
       echo "    sudo go install -tags netgo std"; \
       false; \
}

# If BUILD_IN_CONTAINER is true, the build image is run which launches
# an image that mounts this project as a volume.  The image invokes a build.sh script
# which essentially re-enters this file with BUILD_IN_CONTAINER=FALSE
# causing the else block target below to be called and the files to be built.
# If BUILD_IN_CONTAINER were false to begin with, the else block is
# executed and the binaries are built without ever launching the build container.
ifeq ($(BUILD_IN_CONTAINER),true)

$(EXES) $(DEBUG_EXES) $(PROTO_GOS) $(YACC_GOS) lint test shell check-generated-files: loki-build-image/$(UPTODATE)
	@mkdir -p $(shell pwd)/.pkg
	@mkdir -p $(shell pwd)/.cache
	$(SUDO) docker run $(RM) $(TTY) -i \
		-v $(shell pwd)/.cache:/go/cache \
		-v $(shell pwd)/.pkg:/go/pkg \
		-v $(shell pwd):/go/src/github.com/grafana/loki \
		$(IMAGE_PREFIX)loki-build-image $@;

else

$(DEBUG_EXES): loki-build-image/$(UPTODATE)
	CGO_ENABLED=0 go build $(DEBUG_GO_FLAGS) -o $@ ./$(@D)
	$(NETGO_CHECK)
	# Copy the delve binary to make it easily available to put in the binary's container.
	[ -f "/go/bin/dlv" ] && mv "/go/bin/dlv" $(@D)/dlv

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
	GOGC=20 golangci-lint run

check-generated-files: loki-build-image/$(UPTODATE) yacc protos
	@git diff-files || (echo "changed files; failing check" && exit 1)

test: loki-build-image/$(UPTODATE)
	go test -p=8 ./...

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

push-latest:
	@set -e; \
	for image_name in $(IMAGE_NAMES); do \
		if ! echo $$image_name | grep build; then \
			docker tag $$image_name:$(IMAGE_TAG) $$image_name:latest; \
			docker tag $$image_name:$(IMAGE_TAG) $$image_name:master; \
			docker push $$image_name:latest; \
			docker push $$image_name:master; \
		fi \
	done

helm:
	-rm -f production/helm/*/requirements.lock
	@set -e; \
	helm init -c; \
	for chart in $(CHARTS); do \
		helm lint $$chart; \
		helm dependency build $$chart; \
		helm package $$chart; \
	done
	rm -f production/helm/*/requirements.lock

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

clean:
	$(SUDO) docker rmi $(IMAGE_NAMES) $(DEBUG_IMAGE_NAMES) >/dev/null 2>&1 || true
	rm -rf $(UPTODATE_FILES) $(EXES) $(DEBUG_UPTODATE_FILES) $(DEBUG_EXES) $(DEBUG_DLV_FILES) .cache pkg/promtail/server/ui/assets_vfsdata.go
	go clean ./...

assets:
	@echo ">> writing assets"
	go generate -x -v ./pkg/promtail/server/ui

check_assets: assets
	@echo ">> checking that assets are up-to-date"
	@if ! (cd pkg/promtail/server/ui && git diff --exit-code); then \
		echo "Run 'make assets' and commit the changes to fix the error."; \
		exit 1; \
	fi

helm-install:
	kubectl apply -f tools/helm.yaml
	helm init --wait --service-account helm --upgrade
	$(MAKE) upgrade-helm

helm-debug: ARGS=--dry-run --debug
helm-debug: helm-upgrade

helm-upgrade: helm
	helm upgrade --wait --install $(ARGS) loki-stack ./production/helm/loki-stack \
	--set promtail.image.tag=$(IMAGE_TAG) --set loki.image.tag=$(IMAGE_TAG) -f tools/dev.values.yaml


helm-clean:
	-helm delete --purge loki-stack