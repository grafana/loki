#!/bin/sh
set -e

# This script is used to install the dependencies for the workflows.
# It is intended to be run in a containerized environment, specifically in a
# "golang" container.
# It houses all of the dependencies for the workflows, as well as the dependencies
# needed for the make release-workflows target.

# Set default source directory to GitHub workspace if not provided
SRC_DIR=${SRC_DIR:-${GITHUB_WORKSPACE}}

install_dist_dependencies() {
    # Install Ruby and development dependencies needed for FPM
    apt-get install -y ruby ruby-dev rubygems build-essential

    # Install FPM using gem
    gem install --no-document fpm

    # Install RPM build tools
    apt-get install -y rpm
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
cd "${SRC_DIR}/.github" && jb update -q

# Check if "dist" parameter is passed
if [ "$1" = "dist" ]; then
    install_dist_dependencies
fi
