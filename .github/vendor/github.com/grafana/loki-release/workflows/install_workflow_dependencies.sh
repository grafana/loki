#!/bin/sh
set -e

# This script is used to install the dependencies for the workflows.
# It is intended to be run in a containerized environment, specifically in a
# "golang" container.
# It houses all of the dependencies for the workflows, as well as the dependencies
# needed for the make release-workflows target.
#
# Optional arguments (combinable): dist, lint, loki-release, loki-build-tools.
# Environment: GOLANGCI_LINT_VERSION for lint (default v2.10.1); LYCHEE_VER; BUF_VER.

# Set default source directory to GitHub workspace if not provided
SRC_DIR=${SRC_DIR:-${GITHUB_WORKSPACE}}

# golangci-lint version (e.g. v2.10.1). Override when invoking with the "lint" mode.
GOLANGCI_LINT_VERSION=${GOLANGCI_LINT_VERSION:-v2.10.1}

# Debug information
echo "Current directory: $(pwd)"
echo "SRC_DIR: ${SRC_DIR}"
echo "GITHUB_WORKSPACE: ${GITHUB_WORKSPACE}"

install_dist_dependencies() {
    # Install Ruby and development dependencies needed for FPM
    apt-get install -y ruby ruby-dev rubygems build-essential

    # Install FPM using gem
    gem install --no-document fpm

    # Install RPM build tools
    apt-get install -y rpm
}

install_lint_dependencies() {
    echo "Installing golangci-lint ${GOLANGCI_LINT_VERSION}"
    curl -sfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh |
        sh -s -- -b /usr/local/bin "${GOLANGCI_LINT_VERSION}"
}

install_build_image_tools() {
    # Versions aligned with grafana/loki loki-build-image Dockerfile where applicable.
    LYCHEE_VER=${LYCHEE_VER:-0.7.0}
    BUF_VER=${BUF_VER:-v1.4.0}

    echo "Installing mixtool and goyacc"
    GO111MODULE=on go install github.com/monitoring-mixins/mixtool/cmd/mixtool@16dc166166d91e93475b86b9355a4faed2400c18
    GO111MODULE=on go install golang.org/x/tools/cmd/goyacc@58d531046acdc757f177387bc1725bfa79895d69

    MACHINE=$(uname -m)
    case "${MACHINE}" in
        x86_64)
            LYCHEE_ARCH=x86_64-unknown-linux-gnu
            BUF_ARCH=x86_64
            ;;
        aarch64)
            LYCHEE_ARCH=aarch64-unknown-linux-gnu
            BUF_ARCH=aarch64
            ;;
        *)
            echo "Unsupported machine for lychee/buf downloads: ${MACHINE}" >&2
            exit 1
            ;;
    esac

    echo "Installing lychee ${LYCHEE_VER}"
    curl -fsSL -o /tmp/lychee.tgz \
        "https://github.com/lycheeverse/lychee/releases/download/${LYCHEE_VER}/lychee-${LYCHEE_VER}-${LYCHEE_ARCH}.tar.gz"
    tar -xz -C /tmp -f /tmp/lychee.tgz
    install -m 0755 /tmp/lychee /usr/local/bin/lychee
    rm -f /tmp/lychee.tgz

    echo "Installing buf ${BUF_VER}"
    curl -fsSL -o /usr/local/bin/buf \
        "https://github.com/bufbuild/buf/releases/download/${BUF_VER}/buf-Linux-${BUF_ARCH}"
    chmod 0755 /usr/local/bin/buf
    apt-get install -y protobuf-compiler libprotobuf-dev ragel

    # Install protoc
    # Forcing GO111MODULE=on is required to specify dependencies at specific versions using the go mod notation.
    # If we don't force this, Go is going to default to GOPATH mode as we do not have an active project or go.mod
    # file for it to detect and switch to Go Modules automatically.
    # It's possible this can be revisited in newer versions of Go if the behavior around GOPATH vs GO111MODULES changes
    GO111MODULE=on go install github.com/golang/protobuf/protoc-gen-go@v1.3.1
    GO111MODULE=on go install github.com/gogo/protobuf/protoc-gen-gogoslick@v1.3.0
}

install_loki_release_dependencies() {
    # Install gotestsum
    echo "Installing gotestsum"
    curl -sSfL https://github.com/gotestyourself/gotestsum/releases/download/v1.9.0/gotestsum_1.9.0_linux_amd64.tar.gz | tar -xz -C /usr/local/bin gotestsum

    # Install faillint
    go install github.com/fatih/faillint@latest
}

# Update package lists
apt-get update -qq

# Install tar and xz-utils
apt-get install -qy tar xz-utils

# Install docker
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/debian/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
chmod a+r /etc/apt/keyrings/docker.gpg
# Get codename for Debian - default to "bookworm" if /etc/os-release doesn't exist or VERSION_CODENAME isn't set
CODENAME="bookworm"
if [ -f /etc/os-release ]; then
    # shellcheck source=/dev/null
    . /etc/os-release
    if [ -n "${VERSION_CODENAME:-}" ]; then
        CODENAME="${VERSION_CODENAME}"
    fi
fi
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/debian ${CODENAME} stable" | tee /etc/apt/sources.list.d/docker.list > /dev/null
apt-get update
apt-get install -y docker-ce-cli docker-buildx-plugin

# Install jsonnet
apt-get install -qq -y jsonnet

# Install jsonnet-bundler
go install github.com/jsonnet-bundler/jsonnet-bundler/cmd/jb@latest

# Update jsonnet bundles
if [ -d "${SRC_DIR}/.github" ]; then
    cd "${SRC_DIR}/.github" && jb update -q
else
    echo "Warning: ${SRC_DIR}/.github directory not found, skipping jsonnet bundle update"
fi

# Optional modes (any combination, e.g. "dist lint")
for arg in "$@"; do
    case "${arg}" in
        dist)
            install_dist_dependencies
            ;;
        loki-release)
            install_loki_release_dependencies
            ;;
        lint)
            install_lint_dependencies
            ;;    
        loki-build-tools)
            install_build_image_tools
            ;;
        *)
            echo "Unknown install mode: ${arg}" >&2
            exit 1
            ;;
    esac
done
