# include the bingo binary variables. This enables the bingo versions to be
# referenced here as make variables. For example: $(GOLANGCI_LINT)
include .bingo/Variables.mk

# set the default target here, because the include above will automatically set
# it to the first defined target
.DEFAULT_GOAL := default
default: all

# CLUSTER_LOGGING_VERSION
# defines the version of the OpenShift Cluster Logging product.
# Updates this value when a new version of the product should include this operator and its bundle.
CLUSTER_LOGGING_VERSION ?= 5.1.preview.1

# CLUSTER_LOGGING_NS
# defines the default namespace of the OpenShift Cluster Logging product.
CLUSTER_LOGGING_NS ?= openshift-logging

# VERSION
# defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= v0.0.1
CHANNELS ?= "tech-preview"
DEFAULT_CHANNELS ?= "tech-preview"

# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "preview,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=preview,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="preview,fast,stable")
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

REGISTRY_ORG ?= openshift-logging

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= quay.io/$(REGISTRY_ORG)/loki-operator-bundle:$(VERSION)

GO_FILES := $(shell find . -type f -name '*.go')

# Image URL to use all building/pushing image targets
IMG ?= quay.io/$(REGISTRY_ORG)/loki-operator:$(VERSION)

# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:trivialVersions=true,preserveUnknownFields=false"

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

all: generate lint manager bin/loki-broker

OCI_RUNTIME ?= $(shell which podman || which docker)

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: deps
deps: go.mod go.sum
	go mod tidy
	go mod download
	go mod verify

cli: deps bin/loki-broker ## Build loki-broker CLI binary
bin/loki-broker: $(GO_FILES) | generate
	go build -o $@ ./cmd/loki-broker/

manager: deps generate ## Build manager binary
	go build -o bin/manager main.go

go-generate: ## Run go generate
	go generate ./...

generate: $(CONTROLLER_GEN) ## Generate controller and crd code
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

manifests: $(CONTROLLER_GEN) ## Generate manifests e.g. CRD, RBAC etc.
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

test: deps generate go-generate lint manifests ## Run tests
test: $(GO_FILES)
	go test ./... -coverprofile cover.out

scorecard: generate go-generate bundle ## Run scorecard test
	$(OPERATOR_SDK) scorecard bundle

lint: $(GOLANGCI_LINT) | generate ## Run golangci-lint on source code.
	$(GOLANGCI_LINT) run ./...

fmt: $(GOFUMPT) ## Run gofumpt on source code.
	find . -type f -name '*.go' -not -path '**/fake_*.go' -exec $(GOFUMPT) -s -w {} \;

oci-build: ## Build the image
	$(OCI_RUNTIME) build -t ${IMG} .

oci-push: ## Push the image
	$(OCI_RUNTIME) push ${IMG}

.PHONY: bundle ## Generate bundle manifests and metadata, then validate generated files.
bundle: manifests $(KUSTOMIZE) $(OPERATOR_SDK)
	$(OPERATOR_SDK) generate kustomize manifests -q
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle -q --overwrite --version $(subst v,,$(VERSION)) $(BUNDLE_METADATA_OPTS)
	$(OPERATOR_SDK) bundle validate ./bundle

.PHONY: bundle-build
bundle-build: ## Build the bundle image.
	$(OCI_RUNTIME) build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

##@ Deployment

run: generate manifests ## Run against the configured Kubernetes cluster in ~/.kube/config
	go run ./main.go

install: manifests $(KUSTOMIZE) ## Install CRDs into a cluster
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

uninstall: manifests $(KUSTOMIZE) ## Uninstall CRDs from a cluster
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

deploy: manifests $(KUSTOMIZE) ## Deploy controller in the configured Kubernetes cluster in ~/.kube/config
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/overlays/development | kubectl apply -f -

undeploy: ## Undeploy controller from the configured Kubernetes cluster in ~/.kube/config
	$(KUSTOMIZE) build config/overlays/development | kubectl delete -f -

# Build and push the bundle image to a container registry.
.PHONY: olm-deploy-bundle
olm-deploy-bundle: bundle bundle-build
	$(MAKE) oci-push IMG=$(BUNDLE_IMG)

# Build and push the operator image to a container registry.
.PHONY: olm-deploy-operator
olm-deploy-operator: oci-build oci-push

.PHONY: olm-deploy
ifeq ($(or $(findstring openshift-logging,$(IMG)),$(findstring openshift-logging,$(BUNDLE_IMG))),openshift-logging)
olm-deploy:  ## Deploy the operator bundle and the operator via OLM into an Kubernetes cluster selected via KUBECONFIG.
	$(error Set variable REGISTRY_ORG to use a custom container registry org account for local development)
else
olm-deploy: olm-deploy-bundle olm-deploy-operator $(OPERATOR_SDK)
	kubectl create ns $(CLUSTER_LOGGING_NS)
	kubectl label ns/$(CLUSTER_LOGGING_NS) openshift.io/cluster-monitoring=true --overwrite
	$(OPERATOR_SDK) run bundle -n $(CLUSTER_LOGGING_NS) --install-mode OwnNamespace $(BUNDLE_IMG)
endif

# Build and push the secret for the S3 storage
.PHONY: olm-deploy-example-storage-secret
olm-deploy-example-storage-secret:
	hack/deploy-example-secret.sh $(CLUSTER_LOGGING_NS)

.PHONY: olm-deploy-example
olm-deploy-example: olm-deploy olm-deploy-example-storage-secret ## Deploy example LokiStack custom resource
	kubectl -n $(CLUSTER_LOGGING_NS) create -f hack/lokistack_dev.yaml

.PHONY: olm-undeploy
olm-undeploy: $(OPERATOR_SDK) ## Cleanup deployments of the operator bundle and the operator via OLM on an OpenShift cluster selected via KUBECONFIG.
	$(OPERATOR_SDK) cleanup loki-operator
	kubectl delete ns $(CLUSTER_LOGGING_NS)
