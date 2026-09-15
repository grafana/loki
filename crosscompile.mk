###############################
# Cross-compiling Go binaries #
###############################

# List of binaries to cross-compile
CROSS_BUILD_BINARIES ?= loki logcli lokitool

# Default cross-compile targets (GOOS/GOARCH pairs)
CROSS_BUILD_PLATFORMS ?= linux/amd64 linux/arm64 linux/riscv64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

# Output directory for cross-compiled binaries
CROSS_BUILD_OUTPUT ?= dist

# Generate the list of output file targets from CROSS_BUILD_BINARIES x CROSS_BUILD_PLATFORMS.
# Windows targets get a .exe suffix.
define cross-target-name
$(CROSS_BUILD_OUTPUT)/$(1)-$(subst /,-,$(2))$(if $(filter windows/%,$(2)),.exe)
endef

CROSS_BUILD_TARGETS := $(foreach bin,$(CROSS_BUILD_BINARIES),$(foreach plat,$(CROSS_BUILD_PLATFORMS),$(call cross-target-name,$(bin),$(plat))))

# Each file target depends on the Go sources under its cmd/ directory.
# Usage:
#   make dist/loki-linux-amd64                   # build a single binary
#   make cross-build                              # build all combinations
#   make cross-build CROSS_BUILD_PLATFORMS="linux/amd64 darwin/arm64" CROSS_BUILD_BINARIES="loki logcli"
define cross-build-rule
$(call cross-target-name,$(1),$(2)): $$(shell find cmd/$(1) -name '*.go' 2>/dev/null)
	@mkdir -p $(CROSS_BUILD_OUTPUT)
	@echo "Building $(1) for $(2) -> $$@"
	CGO_ENABLED=0 GOOS=$(word 1,$(subst /, ,$(2))) GOARCH=$(word 2,$(subst /, ,$(2))) go build $$(GO_FLAGS) -o $$@ ./cmd/$(1)
endef

$(foreach bin,$(CROSS_BUILD_BINARIES),$(foreach plat,$(CROSS_BUILD_PLATFORMS),$(eval $(call cross-build-rule,$(bin),$(plat)))))

.PHONY: cross-build
cross-build: $(CROSS_BUILD_TARGETS) ## Cross-compile binaries for specified GOOS/GOARCH platforms
