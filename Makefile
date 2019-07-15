.DEFAULT_GOAL := all
#############
# Variables #
#############

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

# Docker image info
IMAGE_PREFIX ?= grafana
IMAGE_TAG := $(shell ./tools/image-tag)
#GIT_REVISION := $(shell git rev-parse --short HEAD)
#GIT_BRANCH := $(shell git rev-parse --abbrev-ref HEAD)

###################
# Primary Targets #
###################

all: promtail logcli loki
images: promtail-image loki-image

# Binaries
promtail: yacc cmd/promtail/promtail
promtail-debug: yacc cmd/promtail/promtail-debug

logcli: yacc cmd/logcli/logcli

loki: protos yacc cmd/loki/loki
loki-debug: protos yacc cmd/loki/loki-debug

# Images
promtail-image:
	$(SUDO) docker build -t $(IMAGE_PREFIX)/promtail -f cmd/promtail/Dockerfile .
	$(SUDO) docker tag $(IMAGE_PREFIX)/promtail $(IMAGE_PREFIX)/promtail:$(IMAGE_TAG)
promtail-debug-image:
	$(SUDO) docker build -t $(IMAGE_PREFIX)/promtail -f cmd/promtail/Dockerfile.debug .
	$(SUDO) docker tag $(IMAGE_PREFIX)/promtail $(IMAGE_PREFIX)/promtail:$(IMAGE_TAG)

loki-image:
	$(SUDO) docker build -t $(IMAGE_PREFIX)/loki -f cmd/loki/Dockerfile .
	$(SUDO) docker tag $(IMAGE_PREFIX)/loki $(IMAGE_PREFIX)/loki:$(IMAGE_TAG)
loki-debug-image:
	$(SUDO) docker build -t $(IMAGE_PREFIX)/loki -f cmd/loki/Dockerfile.debug .
	$(SUDO) docker tag $(IMAGE_PREFIX)/loki $(IMAGE_PREFIX)/loki:$(IMAGE_TAG)

build-image:
	$(SUDO) docker build -t $(IMAGE_PREFIX)/loki -f loki-build-image/Dockerfile .
	$(SUDO) docker tag $(IMAGE_PREFIX)/loki $(IMAGE_PREFIX)/loki:$(IMAGE_TAG)

# Other builds
protos: $(PROTO_GOS)
yacc: $(YACC_GOS)


# Target to build yacc files
%.y.go: %.y
	goyacc -p $(basename $(notdir $<)) -o $@ $<

# Target to build protobuf files
%.pb.go: $(PROTO_DEFS)
	case "$@" in 	\
  	vendor*)			\
  		protoc -I ./vendor:./$(@D) --gogoslick_out=plugins=grpc:./vendor ./$(patsubst %.pb.go,%.proto,$@); \
  		;;					\
  	*)						\
  		protoc -I ./vendor:./$(@D) --gogoslick_out=Mgoogle/protobuf/timestamp.proto=github.com/gogo/protobuf/types,plugins=grpc:./$(@D) ./$(patsubst %.pb.go,%.proto,$@); \
  		;;					\
  	esac


############
# Promtail #
############

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

##########
# Logcli #
##########

cmd/logcli/logcli: $(APP_GO_FILES) cmd/logcli/main.go
	CGO_ENABLED=0 go build $(GO_FLAGS) -o $@ ./$(@D)
	$(NETGO_CHECK)

########
# Loki #
########

cmd/loki/loki: $(APP_GO_FILES) cmd/loki/main.go
	CGO_ENABLED=0 go build $(GO_FLAGS) -o $@ ./$(@D)
	$(NETGO_CHECK)

cmd/loki/loki-debug: $(APP_GO_FILES) cmd/loki/main.go
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

test:
	go test -p=8 ./...

#########
# Clean #
#########

clean:
	rm -rf cmd/promtail/promtail pkg/promtail/server/ui/assets_vfsdata.go
	rm -rf cmd/loki/loki
	rm -rf cmd/logcli/logcli
	rm -rf .cache
	go clean ./...



helm-install:
	kubectl apply -f tools/helm.yaml
	helm init --wait --service-account helm --upgrade
	$(MAKE) helm-upgrade

helm-debug: ARGS=--dry-run --debug
helm-debug: helm-upgrade

helm-upgrade:
	helm upgrade --wait --install $(ARGS) loki-stack ./production/helm/loki-stack \
	--set promtail.image.tag=$(IMAGE_TAG) --set loki.image.tag=$(IMAGE_TAG) -f tools/dev.values.yaml


helm-clean:
	-helm delete --purge loki-stack
