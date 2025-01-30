.PHONY: build
build:
	npm install
	npm run build

.PHONY: dev
dev:
	npm run dev

.PHONY: clean
clean:
	rm -rf node_modules
	rm -rf dist

.PHONY: check-deps
check-deps:
	npx depcheck
	npm audit
