.DEFAULT_GOAL := all
.PHONY: all images check-generated-files logcli loki loki-debug promtail promtail-debug loki-canary lint test clean yacc protos
.PHONY: helm helm-install helm-upgrade helm-publish helm-debug helm-clean
.PHONY: docker-driver docker-driver-clean docker-driver-enable docker-driver-push
.PHONY: push-images push-latest save-images load-images promtail-image loki-image build-image
.PHONY: benchmark-store
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
BUILD_IMAGE_VERSION := "0.2.1"

# Docker image info
IMAGE_PREFIX ?= grafana
IMAGE_TAG := $(shell ./tools/image-tag)

# Version info for binaries
GIT_REVISION := $(shell git rev-parse --short HEAD)
GIT_BRANCH := $(shell git rev-parse --abbrev-ref HEAD)

# We don't want find to scan inside a bunch of directories, to accelerate the
# 'make: Entering directory '/go/src/github.com/grafana/loki' phase.
DONT_FIND := -name tools -prune -o -name vendor -prune -o -name .git -prune -o -name .cache -prune -o -name .pkg -prune -o

# These are all the application files, they are included in the various binary rules as dependencies
# to make sure binaries are rebuilt if any source files change.
APP_GO_FILES := $(shell find . $(DONT_FIND) -name .y.go -prune -o -name .pb.go -prune -o -name cmd -prune -o -type f -name '*.go' -print)

# Build flags
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

# Protobuf files
PROTO_DEFS := $(shell find . $(DONT_FIND) -type f -name '*.proto' -print)
PROTO_GOS := $(patsubst %.proto,%.pb.go,$(PROTO_DEFS))

# Yacc Files
YACC_DEFS := $(shell find . $(DONT_FIND) -type f -name *.y -print)
YACC_GOS := $(patsubst %.y,%.y.go,$(YACC_DEFS))

# RM is parameterized to allow CircleCI to run builds, as it
# currently disallows `docker run --rm`. This value is overridden
# in circle.yml
RM := --rm
# TTY is parameterized to allow Google Cloud Builder to run builds,
# as it currently disallows TTY devices. This value needs to be overridden
# in any custom cloudbuild.yaml files
TTY := --tty

################
# Main Targets #
################

all: promtail logcli loki loki-canary check-generated-files


# This is really a check for the CI to make sure generated files are built and checked in manually
check-generated-files: yacc protos
	@if ! (git diff --exit-code $(YACC_GOS) $(PROTO_GOS)); then \
		echo "\nChanges found in either generated protos or yaccs"; \
		echo "Run 'make all' and commit the changes to fix this error."; \
		echo "If you are actively developing these files you can ignore this error"; \
		echo "(Don't forget to check in the generated files when finished)\n"; \
		exit 1; \
	fi


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

promtail: yacc cmd/promtail/promtail
promtail-debug: yacc cmd/promtail/promtail-debug

# Rule to generate promtail static assets file
pkg/promtail/server/ui/assets_vfsdata.go:
	@echo ">> writing assets"
	GOOS=$(shell go env GOHOSTOS) go generate -x -v ./pkg/promtail/server/ui

cmd/promtail/promtail: $(APP_GO_FILES) pkg/promtail/server/ui/assets_vfsdata.go cmd/promtail/main.go
	CGO_ENABLED=0 go build $(GO_FLAGS) -o $@ ./$(@D)
	$(NETGO_CHECK)

cmd/promtail/promtail-debug: $(APP_GO_FILES) pkg/promtail/server/ui/assets_vfsdata.go cmd/promtail/main.go
	CGO_ENABLED=0 go build $(DEBUG_GO_FLAGS) -o $@ ./$(@D)
	$(NETGO_CHECK)

########
# Lint #
########

lint:
	GOGC=20 golangci-lint run

########
# Test #
########

test: all
	go test -p=8 ./...

#########
# Clean #
#########

clean:
	rm -rf cmd/promtail/promtail pkg/promtail/server/ui/assets_vfsdata.go
	rm -rf cmd/loki/loki
	rm -rf cmd/logcli/logcli
	rm -rf cmd/loki-canary/loki-canary
	rm -rf .cache
	rm -rf cmd/docker-driver/rootfs
	go clean ./...

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
		-v $(shell pwd):/go/src/github.com/grafana/loki \
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
		-v $(shell pwd):/go/src/github.com/grafana/loki \
		$(IMAGE_PREFIX)/loki-build-image:$(BUILD_IMAGE_VERSION) $@;
else
	case "$@" in 	\
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

CHARTS := production/helm/loki production/helm/promtail production/helm/loki-stack

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
	$(MAKE) helm-upgrade

helm-upgrade: helm
	helm upgrade --wait --install $(ARGS) loki-stack ./production/helm/loki-stack \
	--set promtail.image.tag=$(IMAGE_TAG) --set loki.image.tag=$(IMAGE_TAG) -f tools/dev.values.yaml

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

PLUGIN_TAG ?= $(IMAGE_TAG)

docker-driver: docker-driver-clean cmd/docker-driver/docker-driver
	mkdir cmd/docker-driver/rootfs
	docker build -t rootfsimage cmd/docker-driver
	ID=$$(docker create rootfsimage true) && \
	(docker export $$ID | tar -x -C cmd/docker-driver/rootfs) && \
	docker rm -vf $$ID
	docker rmi rootfsimage -f
	docker plugin create grafana/loki-docker-driver:$(PLUGIN_TAG) cmd/docker-driver

cmd/docker-driver/docker-driver: $(APP_GO_FILES)
	CGO_ENABLED=0 go build $(GO_FLAGS) -o $@ ./$(@D)
	$(NETGO_CHECK)

docker-driver-push: docker-driver
	docker plugin push grafana/loki-docker-driver:$(PLUGIN_TAG)

docker-driver-enable:
	docker plugin enable grafana/loki-docker-driver:$(PLUGIN_TAG)

docker-driver-clean:
	-docker plugin disable grafana/loki-docker-driver:$(IMAGE_TAG)
	-docker plugin rm grafana/loki-docker-driver:$(IMAGE_TAG)
	rm -rf cmd/docker-driver/rootfs


##########
# Images #
##########

images: promtail-image loki-image loki-canary-image docker-driver

IMAGE_NAMES := grafana/loki grafana/promtail grafana/loki-canary

save-images:
	@set -e; \
	mkdir -p images; \
	for image_name in $(IMAGE_NAMES); do \
		echo ">> saving image $$image_name:$(IMAGE_TAG)"; \
		docker save $$image_name:$(IMAGE_TAG) -o images/$$(echo $$image_name | tr "/" _):$(IMAGE_TAG); \
	done

load-images:
	@set -e; \
	mkdir -p images; \
	for image_name in $(IMAGE_NAMES); do \
		docker load -i images/$$(echo $$image_name | tr "/" _):$(IMAGE_TAG); \
	done

push-images:
	@set -e; \
	for image_name in $(IMAGE_NAMES); do \
		docker push $$image_name:$(IMAGE_TAG); \
	done

push-latest:
	@set -e; \
	for image_name in $(IMAGE_NAMES); do \
		docker tag $$image_name:$(IMAGE_TAG) $$image_name:latest; \
		docker tag $$image_name:$(IMAGE_TAG) $$image_name:master; \
		docker push $$image_name:latest; \
		docker push $$image_name:master; \
	done


promtail-image:
	$(SUDO) docker build -t $(IMAGE_PREFIX)/promtail -f cmd/promtail/Dockerfile .
	$(SUDO) docker tag $(IMAGE_PREFIX)/promtail $(IMAGE_PREFIX)/promtail:$(IMAGE_TAG)
promtail-debug-image:
	$(SUDO) docker build -t $(IMAGE_PREFIX)/promtail -f cmd/promtail/Dockerfile.debug .
	$(SUDO) docker tag $(IMAGE_PREFIX)/promtail-debug $(IMAGE_PREFIX)/promtail-debug:$(IMAGE_TAG)

loki-image:
	$(SUDO) docker build -t $(IMAGE_PREFIX)/loki -f cmd/loki/Dockerfile .
	$(SUDO) docker tag $(IMAGE_PREFIX)/loki $(IMAGE_PREFIX)/loki:$(IMAGE_TAG)
loki-debug-image:
	$(SUDO) docker build -t $(IMAGE_PREFIX)/loki -f cmd/loki/Dockerfile.debug .
	$(SUDO) docker tag $(IMAGE_PREFIX)/loki-debug $(IMAGE_PREFIX)/loki-debug:$(IMAGE_TAG)

loki-canary-image:
	$(SUDO) docker build -t $(IMAGE_PREFIX)/loki-canary -f cmd/loki-canary/Dockerfile .
	$(SUDO) docker tag $(IMAGE_PREFIX)/loki-canary $(IMAGE_PREFIX)/loki-canary:$(IMAGE_TAG)

build-image:
	$(SUDO) docker build -t $(IMAGE_PREFIX)/loki-build-image -f loki-build-image/Dockerfile .
	$(SUDO) docker tag $(IMAGE_PREFIX)/loki-build-image $(IMAGE_PREFIX)/loki-build-image:$(IMAGE_TAG)


########
# Misc #
########

benchmark-store:
	go run ./pkg/storage/hack/main.go
	go test ./pkg/storage/ -bench=.  -benchmem -memprofile memprofile.out -cpuprofile cpuprofile.out
