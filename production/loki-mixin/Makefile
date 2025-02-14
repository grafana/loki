.DEFAULT_GOAL:= lint
PATH := ./node_modules/.bin:$(PATH)
SHELL := /bin/bash
args = $(filter-out $@, $(MAKECMDGOALS))
.PHONY: all setup install clean reinstall lint lint-sh lint-shell lint-md lint-markdown lint-txt lint-text lint-yaml lint-yml lint-editorconfig lint-ec lint-jsonnet ci-lint ci-lint-shell ci-lint-markdown ci-lint-text ci-lint-yaml ci-lint-editorconfig tests ci-tests ci ci-install

default: all

all: install

####################################################################
#                   Installation / Setup                           #
####################################################################
setup:
	@./tools/setup.sh

install:
	jb install

# remove the build and log folders
clean:
	rm -rf vendor dashboards_out alerts.yaml rules.yaml

# reinstall the node_modules and start with a fresh node build
reinstall: clean install

####################################################################
#                           Linting / Formatting                   #
####################################################################
lint: lint-jsonnet

lint-jsonnet:
	@./tools/lint-jsonnet.sh

fmt:
	@find . -name 'vendor' -prune -o -name '*.libsonnet' -print -o -name '*.jsonnet' -print | \
		xargs -n 1 -- jsonnetfmt -i


####################################################################
#                              Tests                                  #
####################################################################
tests:
	@./tools/run-tests.sh

####################################################################
#                              CI                                  #
####################################################################
ci: ci-lint ci-tests

ci-lint: ci-lint-jsonnet

# Jsonnet Linting
ci-lint-jsonnet:
	@./tools/lint-jsonnet.sh

# Test
ci-tests:
	@./tools/run-tests.sh
