IMAGE = grafana/docs-base:latest

.PHONY: pull
pull:
	docker pull ${IMAGE}

.PHONY: docs
docs: pull
	docker run  --rm -it -v ${PWD}/sources:/hugo/content/docs/loki/latest -p 3002:3002 $(IMAGE)

.PHONY: docs-next
docs-next: pull
	docker run  --rm -it -v ${PWD}/sources:/hugo/content/docs/loki/next -p 3002:3002 $(IMAGE)

.PHONY: docs-test
docs-test: pull
	docker run --rm -it -v ${PWD}/sources:/hugo/content/docs/loki/latest -p 3002:3002 $(IMAGE) /bin/bash -c 'make prod'
