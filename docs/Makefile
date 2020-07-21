IMAGE = grafana/docs-base:latest

.PHONY: docs
docs:
	docker pull ${IMAGE}
	docker run -v ${PWD}/sources:/hugo/content/docs/loki/latest -p 3002:3002 --rm $(IMAGE) /bin/bash -c 'mkdir -p content/docs/grafana/latest/ && touch content/docs/grafana/latest/menu.yaml && make server'

.PHONY: docs-test
docs-test:
	docker pull ${IMAGE}
	docker run -v ${PWD}/sources:/hugo/content/docs/loki/latest -p 3002:3002 --rm $(IMAGE) /bin/bash -c 'mkdir -p content/docs/grafana/latest/ && touch content/docs/grafana/latest/menu.yaml && make prod'