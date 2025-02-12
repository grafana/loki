#!/usr/bin/env bash

source "$(pwd)/tools/includes/utils.sh"

source "./tools/includes/logging.sh"

# output the heading
heading "Loki Mixin" "Performing Setup Checks"

# make sure go exists
info "Checking to see if go is installed"
if [[ "$(command -v go)" = "" ]]; then
  emergency "go command is required, see: (https://go.dev) or run: brew install go";
else
  success "go is installed"
fi

# make sure jsonnet exists
info "Checking to see if jsonnet is installed"
if [[ "$(command -v jsonnet)" = "" ]]; then
  emergency "jsonnet command is required, see: (https://github.com/google/go-jsonnet) or run: go install github.com/google/go-jsonnet/cmd/jsonnet@latest";
else
  success "jsonnet is installed"
fi

# make sure jsonnet-linter exists
info "Checking to see if jsonnet-lint is installed"
if [[ "$(command -v jsonnet-lint)" = "" ]]; then
  emergency "jsonnet-lint command is required, see: (https://github.com/google/go-jsonnet/blob/master/linter/README.md) or run: go install github.com/google/go-jsonnet/cmd/jsonnet-lint@latest";
else
  success "jsonnet-lint is installed"
fi

# make sure Node exists
info "Checking to see if Node is installed"
if [[ "$(command -v node)" = "" ]]; then
  warning "node is required if running lint locally, see: (https://nodejs.org) or run: brew install nvm && nvm install 18";
else
  success "node is installed"
fi

# make sure jb exists
info "Checking to see if jb is installed"
if [[ "$(command -v jb)" = "" ]]; then
  emergency "jb command is required, see: (https://github.com/jsonnet-bundler/jsonnet-bundler) or run: go install -a github.com/jsonnet-bundler/jsonnet-bundler/cmd/jb@latest";
else
  success "jb is installed"
fi
