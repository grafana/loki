# include the bingo binary variables. This enables the bingo versions to be
# referenced here as make variables. For example: $(GOLANGCI_LINT)
include .bingo/Variables.mk

WEBSITE_DIR ?= website
WEBSITE_BASE_URL ?= https://loki-operator.dev

# set the default target here, because the include above will automatically set
# it to the first defined target
.DEFAULT_GOAL := default
default: all

# LOKI_OPERATOR_NS
# defines the default namespace of the Loki Operator in OpenShift.
# Loki Operator will be installed in this namespace.
LOKI_OPERATOR_NS ?= kubernetes-operators

# VERSION
# defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 0.6.2
CHANNELS ?= "alpha"
DEFAULT_CHANNEL ?= "alpha"

# REGISTRY_BASE
# defines the container registry and organization for the bundle and operator container images.
REGISTRY_BASE_COMMUNITY = docker.io/grafana
REGISTRY_BASE_OPENSHIFT = quay.io/openshift-logging
REGISTRY_BASE ?= $(REGISTRY_BASE_COMMUNITY)

# Customize for variants: community, community-openshift or openshift
VARIANT ?= community
ifeq ($(VARIANT), openshift)
ifeq ($(REGISTRY_BASE), $(REGISTRY_BASE_COMMUNITY))
    REGISTRY_BASE = $(REGISTRY_BASE_OPENSHIFT)
endif
    VERSION = 0.1.0
    CHANNELS = stable
    DEFAULT_CHANNEL = stable
    LOKI_OPERATOR_NS = openshift-operators-redhat
endif

# Image URL to use all building/pushing image targets
IMG ?= $(REGISTRY_BASE)/loki-operator:$(VERSION)

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

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(REGISTRY_BASE)/loki-operator-bundle:$(VERSION)

# BUNDLE_GEN_FLAGS are the flags passed to the operator-sdk generate bundle command
BUNDLE_GEN_FLAGS ?= -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)

MANIFESTS_DIR = config/manifests/$(VARIANT)
BUNDLE_DIR = ./bundle/$(VARIANT)
BUNDLE_BUILD_GEN_FLAGS ?= $(BUNDLE_GEN_FLAGS) --output-dir . --kustomize-dir ../../$(MANIFESTS_DIR)

# USE_IMAGE_DIGESTS defines if images are resolved via tags or digests
# You can enable this value if you would like to use SHA Based Digests
# To enable set flag to true
USE_IMAGE_DIGESTS ?= false
ifeq ($(USE_IMAGE_DIGESTS), true)
    BUNDLE_GEN_FLAGS += --use-image-digests
endif

CALCULATOR_IMG ?= $(REGISTRY_BASE)/storage-size-calculator:latest

GO_FILES := $(shell find . -type f -name '*.go')

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

.PHONY: all
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

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-24s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: deps
deps: go.mod go.sum
	go mod tidy
	go mod download
	go mod verify

.PHONY: deps-api
deps-api: apis/loki/go.mod apis/loki/go.sum
	@cd ./apis/loki/ && go mod tidy
	@cd ./apis/loki/ && go mod download
	@cd ./apis/loki/ && go mod verify

.PHONY: cli
cli: deps bin/loki-broker ## Build loki-broker CLI binary
bin/loki-broker: $(GO_FILES) | generate
	go build -o $@ ./cmd/loki-broker/

.PHONY: manager
manager: deps generate ## Build manager binary
	go build -o bin/manager main.go

.PHONY: size-calculator
size-calculator: deps generate ## Build size-calculator binary
	go build -o bin/size-calculator main.go

.PHONY: go-generate
go-generate: ## Run go generate
	go generate ./...

.PHONY: generate
generate: $(CONTROLLER_GEN) ## Generate controller and crd code
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: manifests
manifests: $(CONTROLLER_GEN) ## Generate manifests e.g. CRD, RBAC etc.
	$(CONTROLLER_GEN) rbac:roleName=lokistack-manager crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: test
test: deps deps-api generate go-generate lint lint-prometheus manifests test-unit-api ## Run tests
test: $(GO_FILES)
	go test ./... -coverprofile cover.out

.PHONY: test-unit-api
test-unit-api: $(GO_FILES)
	@cd ./apis/loki/ && go test ./... -coverprofile cover.out

.PHONY: test-unit-prometheus
test-unit-prometheus: $(PROMTOOL) ## Run prometheus unit tests
	@$(PROMTOOL) test rules ./internal/manifests/internal/alerts/testdata/test.yaml

.PHONY: scorecard
scorecard: generate go-generate bundle-all ## Run scorecard tests for all bundles (community, community-openshift, openshift)
	$(OPERATOR_SDK) scorecard -c ./bundle/community/tests/scorecard/config.yaml bundle/community
	$(OPERATOR_SDK) scorecard -c ./bundle/community-openshift/tests/scorecard/config.yaml bundle/community-openshift
	$(OPERATOR_SDK) scorecard -c ./bundle/openshift/tests/scorecard/config.yaml bundle/openshift

.PHONY: lint
lint: $(GOLANGCI_LINT) | generate ## Run golangci-lint on source code.
	$(GOLANGCI_LINT) run --timeout=5m ./...

.PHONY: lint-fix
lint-fix: $(GOLANGCI_LINT) ## Attempt to automatically fix lint issues in source code.
	$(GOLANGCI_LINT) run --fix --timeout=5m ./...

.PHONY: lint-prometheus
lint-prometheus: $(PROMTOOL) ## Run promtool check against recording rules and alerts.
	@$(PROMTOOL) check rules ./internal/manifests/internal/alerts/prometheus-*.yaml

.PHONY: fmt
fmt: $(GOFUMPT) ## Run gofumpt on source code.
	find . -type f -name '*.go' -not -path '**/fake_*.go' -exec $(GOFUMPT) -w {} \;

.PHONY: oci-build
oci-build: ## Build the image
	$(OCI_RUNTIME) build -t ${IMG} .

.PHONY: oci-push
oci-push: ## Push the image
	$(OCI_RUNTIME) push ${IMG}

.PHONY: bundle-all
bundle-all: ## Generate both bundles.
	$(MAKE) bundle
	$(MAKE) bundle VARIANT=community-openshift
	$(MAKE) bundle VARIANT=openshift

.PHONY: bundle
bundle: manifests $(KUSTOMIZE) $(OPERATOR_SDK) ## Generate variant bundle manifests and metadata, then validate generated files.
	$(OPERATOR_SDK) generate kustomize manifests -q --input-dir $(MANIFESTS_DIR) --output-dir $(MANIFESTS_DIR)
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	cd $(BUNDLE_DIR) && cp ../../PROJECT . && $(KUSTOMIZE) build ../../$(MANIFESTS_DIR) | $(OPERATOR_SDK) generate bundle $(BUNDLE_BUILD_GEN_FLAGS) && rm PROJECT
	$(OPERATOR_SDK) bundle validate $(BUNDLE_DIR)

.PHONY: bundle-build
bundle-build: ## Build the community bundle image
	$(OCI_RUNTIME) build -f $(BUNDLE_DIR)/bundle.Dockerfile -t $(BUNDLE_IMG) $(BUNDLE_DIR)

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: quickstart
ifeq ($(or $(findstring openshift-logging,$(IMG)),$(findstring openshift-logging,$(BUNDLE_IMG))),openshift-logging)
quickstart: $(KIND) ## Quickstart full dev environment on local kind cluster
	@./quickstart.sh $(filter-out $@,$(MAKECMDGOALS))
else
quickstart: oci-build oci-push $(KIND)
	@./quickstart.sh $(filter-out $@,$(MAKECMDGOALS))
endif

.PHONY: quickstart-cleanup
quickstart-cleanup: $(KIND) ## Cleanup for quickstart set up
	$(KIND) delete cluster

.PHONY: run
run: generate manifests ## Run against the configured Kubernetes cluster in ~/.kube/config
	go run ./main.go

.PHONY: install
install: manifests $(KUSTOMIZE) ## Install CRDs into a cluster
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

.PHONY: uninstall
uninstall: manifests $(KUSTOMIZE) ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests $(KUSTOMIZE) ## Deploy controller in the configured Kubernetes cluster in ~/.kube/config
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/overlays/development | kubectl apply -f -

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/overlays/development | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

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
	$(error Set variable REGISTRY_BASE to use a custom container registry org account for local development)
else
olm-deploy: olm-deploy-bundle olm-deploy-operator $(OPERATOR_SDK)
	$(OPERATOR_SDK) run bundle -n $(LOKI_OPERATOR_NS) --install-mode AllNamespaces $(BUNDLE_IMG) --security-context-config restricted
endif

.PHONY: olm-upgrade
ifeq ($(or $(findstring openshift-logging,$(IMG)),$(findstring openshift-logging,$(BUNDLE_IMG))),openshift-logging)
olm-upgrade:  ## Upgrade the operator bundle and the operator via OLM into an Kubernetes cluster selected via KUBECONFIG.
	$(error Set variable REGISTRY_BASE to use a custom container registry org account for local development)
else
olm-upgrade: olm-deploy-bundle olm-deploy-operator $(OPERATOR_SDK)
	$(OPERATOR_SDK) run bundle-upgrade -n $(LOKI_OPERATOR_NS) $(BUNDLE_IMG)
endif

.PHONY: olm-undeploy
olm-undeploy: $(OPERATOR_SDK) ## Cleanup deployments of the operator bundle and the operator via OLM on an OpenShift cluster selected via KUBECONFIG.
	$(OPERATOR_SDK) cleanup -n $(LOKI_OPERATOR_NS) loki-operator

.PHONY: deploy-size-calculator
ifeq ($(findstring openshift-logging,$(CALCULATOR_IMG)),openshift-logging)
deploy-size-calculator: ## Deploy storage size calculator (OpenShift only!)
	$(error Set variable REGISTRY_BASE to use a custom container registry org account for local development)
else
deploy-size-calculator:  $(KUSTOMIZE) ## Deploy storage size calculator (OpenShift only!)
	kubectl apply -f config/overlays/openshift/size-calculator/cluster_monitoring_config.yaml
	kubectl apply -f config/overlays/openshift/size-calculator/user_workload_monitoring_config.yaml
	./hack/deploy-prometheus-secret.sh
	$(KUSTOMIZE) build config/overlays/openshift/size-calculator | kubectl apply -f -
endif

.PHONY: undeploy-size-calculator
undeploy-size-calculator: ## Undeploy storage size calculator
	$(KUSTOMIZE) build config/overlays/openshift/size-calculator | kubectl delete -f -

.PHONY: oci-build-calculator
oci-build-calculator: ## Build the calculator image
	$(OCI_RUNTIME) build -f calculator.Dockerfile -t $(CALCULATOR_IMG) .

.PHONY: oci-push-calculator
oci-push-calculator: ## Push the calculator image
	$(OCI_RUNTIME) push $(CALCULATOR_IMG)

##@ Website
TYPES_TARGET := $(shell find apis/loki -type f -iname "*_types.go")
docs/operator/api.md: $(TYPES_TARGET) $(GEN_CRD_API_REFERENCE_DOCS)
	$(GEN_CRD_API_REFERENCE_DOCS) -api-dir "github.com/grafana/loki/operator/apis/loki/" -config "$(PWD)/config/docs/config.json" -template-dir "$(PWD)/config/docs/templates" -out-file "$(PWD)/$@"
	sed -i 's/+docs:/  docs:/' $@
	sed -i 's/+parent:/    parent:/' $@
	sed -i 's/##/\n##/' $@
	sed -i 's/+newline/\n/' $@

FEATURE_GATES_TARGET := $(shell find apis/config -type f -iname "*_types.go")
docs/operator/feature-gates.md: $(FEATURE_GATES_TARGET) $(GEN_CRD_API_REFERENCE_DOCS)
	$(GEN_CRD_API_REFERENCE_DOCS) -api-dir "github.com/grafana/loki/operator/apis/config/v1/" -config "$(PWD)/config/docs/config.json" -template-dir "$(PWD)/config/docs/templates" -out-file "$(PWD)/$@"
	sed -i 's/title: "API"/title: "Feature Gates"/' $@
	sed -i 's/+docs:/  docs:/' $@
	sed -i 's/+parent:/    parent:/' $@
	sed -i 's/##/\n##/' $@
	sed -i 's/+newline/\n/' $@

.PHONY: web-pre
web-pre: docs/operator/api.md docs/operator/feature-gates.md
	@echo ">> preprocessing docs for website"
	@git submodule update --init --recursive
	cd $(WEBSITE_DIR)/themes/doks/ && npm install && rm -rf content

.PHONY: web
web: $(HUGO) | web-pre ## Run production build of the loki-operator.dev website
	cd $(WEBSITE_DIR) && $(HUGO) -b $(WEBSITE_BASE_URL)

.PHONY: web-serve
web-serve: $(HUGO) | web-pre ## Run local preview version of the loki-operator.dev website
	@cd $(WEBSITE_DIR) && $(HUGO) serve

JSONNET_SRC = $(shell find . -type f -not -path './jsonnet/vendor/*' \( -name '*.libsonnet' -o -name '*.jsonnet' \))
JSONNET_VENDOR_DIR = jsonnet/vendor

$(JSONNET_VENDOR_DIR): $(JB) jsonnet/jsonnetfile.json jsonnet/jsonnetfile.lock.json
	@cd jsonnet/ && $(JB) install

jsonnet-format: $(JSONNETFMT) $(JSONNET_SRC)
	@$(JSONNETFMT) -n 2 --max-blank-lines 2 --string-style s --comment-style s -i $(JSONNET_SRC)

generate-dashboards: $(JSONNET) jsonnet-format $(JSONNET_VENDOR_DIR)
	@rm -f internal/manifests/openshift/internal/dashboards/static/*.json
	@$(JSONNET) -J "$(JSONNET_VENDOR_DIR)" -m internal/manifests/openshift/internal/dashboards/static jsonnet/main.jsonnet
