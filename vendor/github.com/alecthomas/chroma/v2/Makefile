.PHONY: chromad upload all

VERSION ?= $(shell git describe --tags --dirty  --always)
export GOOS ?= linux
export GOARCH ?= amd64

all: README.md tokentype_string.go

README.md: lexers/*/*.go
	./table.py

tokentype_string.go: types.go
	go generate

.PHONY: chromad
chromad: build/chromad

build/chromad: $(shell find cmd/chromad -name '*.go' -o -name '*.html' -o -name '*.css' -o -name '*.js') \
	cmd/chromad/static/wasm_exec.js \
	cmd/chromad/static/chroma.wasm
	rm -rf build
	esbuild --platform=node --bundle cmd/chromad/static/index.js --minify --outfile=cmd/chromad/static/index.min.js
	esbuild --bundle cmd/chromad/static/index.css --minify --outfile=cmd/chromad/static/index.min.css
	(export CGOENABLED=0 ; go build -C cmd/chromad -ldflags="-X 'main.version=$(VERSION)'" -o ../../build/chromad .)

cmd/chromad/static/wasm_exec.js: $(shell tinygo env TINYGOROOT)/targets/wasm_exec.js
	install -m644 $< $@

cmd/chromad/static/chroma.wasm: cmd/libchromawasm/main.go
	tinygo build -no-debug -target wasm -o $@ $<

upload: build/chromad
	scp build/chromad root@swapoff.org: && \
		ssh root@swapoff.org 'install -m755 ./chromad /srv/http/swapoff.org/bin && service chromad restart'
