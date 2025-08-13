.PHONY: prepare
prepare:
	npm install

.PHONY: build
build: prepare
	npm run build

.PHONY: dev
dev: prepare
	npm run dev

.PHONY: clean
clean:
	rm -rf node_modules
	rm -rf dist

.PHONY: check-deps
check-deps: prepare
	npx depcheck
	npm audit

.PHONY: lint
lint: prepare
	npm run lint

.PHONY: test
test: prepare
	npm run test:ci
