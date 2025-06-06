#!/bin/sh
set -e

# This script is used to install the dependencies for the workflows.
# It is intended to be run in a containerized environment, specifically in a
# "golang" container.
# It houses all of the dependencies for the workflows, as well as the dependencies
# needed for the make release-workflows target.

# Set default source directory to GitHub workspace if not provided
SRC_DIR=${SRC_DIR:-${GITHUB_WORKSPACE}}

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

# Check if "dist" parameter is passed
if [ "$1" = "dist" ]; then
    install_dist_dependencies
fi
if [ "$1" = "loki-release" ]; then
    install_loki_release_dependencies
fi
