IMAGE = grafana/grafana-docs-dev:latest

.PHONY: docs
docs:
	docker pull ${IMAGE}
	docker run  --rm -it -v ${PWD}/sources:/hugo/content/docs/loki/latest -p 3002:3002 $(IMAGE) /bin/bash -c 'mkdir -p content/docs/grafana/latest/ && touch content/docs/grafana/latest/menu.yaml && make server-quick'

.PHONY: docs-next
docs-next:
	docker pull ${IMAGE}
	docker run  --rm -it -v ${PWD}/sources:/hugo/content/docs/loki/next -p 3002:3002 $(IMAGE) /bin/bash -c 'mkdir -p content/docs/grafana/next/ && touch content/docs/grafana/next/menu.yaml && make server-quick'

.PHONY: docs-test
docs-test:
	docker pull ${IMAGE}
	docker run --rm -it -v ${PWD}/sources:/hugo/content/docs/loki/latest -p 3002:3002 $(IMAGE) /bin/bash -c 'mkdir -p content/docs/grafana/latest/ && touch content/docs/grafana/latest/menu.yaml && make prod'

