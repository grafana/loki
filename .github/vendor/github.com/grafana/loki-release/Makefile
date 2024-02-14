test:
	echo "testing"

lint:
	echo "linting"

lint-jsonnet:
	echo "linting jsonnet"

loki:
	go build -o cmd/loki/loki cmd/loki/main.go

loki-image:
	docker build -t trevorwhitney075/loki -f cmd/loki/Dockerfile .

clean:
	rm -rf cmd/loki/loki dist

check-generated-files:
	echo "checking generated files"

check-mod:
	echo "checking mod"

lint-scripts:
	echo "linting scripts"

check-doc:
	echo "checking docs"

check-example-config-doc:
	echo "checking example config docs"

documentation-helm-reference-check:
	echo "documentation helm reference check"

dist:
	mkdir -p dist
	cp CHANGELOG.md dist/

packages:
	mkdir -p dist
	cp CHANGELOG.md dist/
