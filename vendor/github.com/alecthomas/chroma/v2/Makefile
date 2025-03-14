.PHONY: chromad upload all

VERSION ?= $(shell git describe --tags --dirty  --always)
export GOOS ?= linux
export GOARCH ?= amd64

all: README.md tokentype_string.go

README.md: lexers/*/*.go
	./table.py

tokentype_string.go: types.go
	go generate

chromad:
	rm -rf build
	esbuild --bundle cmd/chromad/static/index.js --minify --outfile=cmd/chromad/static/index.min.js
	esbuild --bundle cmd/chromad/static/index.css --minify --outfile=cmd/chromad/static/index.min.css
	(export CGOENABLED=0 ; cd ./cmd/chromad && go build -ldflags="-X 'main.version=$(VERSION)'" -o ../../build/chromad .)

upload: build/chromad
	scp build/chromad root@swapoff.org: && \
		ssh root@swapoff.org 'install -m755 ./chromad /srv/http/swapoff.org/bin && service chromad restart'
