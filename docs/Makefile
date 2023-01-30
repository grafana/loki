PODMAN = $(shell if command -v podman &>/dev/null; then echo podman; else echo docker; fi)
IMAGE = grafana/docs-base:latest
BUILD_IN_CONTAINER ?= true

.PHONY: pull
pull:
	$(PODMAN) pull ${IMAGE}

.PHONY: docs
docs: pull
	$(PODMAN) run  --rm -it -v ${PWD}/sources:/hugo/content/docs/loki/latest -p 3002:3002 $(IMAGE)

.PHONY: docs-next
docs-next: pull
	$(PODMAN) run  --rm -it -v ${PWD}/sources:/hugo/content/docs/loki/next -p 3002:3002 $(IMAGE)

.PHONY: docs-test
docs-test: pull
	$(PODMAN) run --rm -it -v ${PWD}/sources:/hugo/content/docs/loki/latest -p 3002:3002 $(IMAGE) /bin/bash -c 'make prod'

sources/installation/helm/reference.md: ../production/helm/loki/reference.md.gotmpl
ifeq ($(BUILD_IN_CONTAINER),true)
	$(PODMAN) run --rm --volume "$(realpath ..):/helm-docs:z" -u "$$(id -u)" "docker.io/jnorwood/helm-docs:v1.11.0" \
		-c /helm-docs/production/helm/ \
		-t reference.md.gotmpl \
		-o reference.md
else
	helm-docs -c ../production/helm/ -t reference.md.gotmpl -o reference.md
endif
	mv "$(basename $<)" "$@"
