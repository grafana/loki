# Copyright 2024 The Libc Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

.PHONY:	all build_all_targets check clean download edit editor generate dev membrk-test test work xtest short-test xlibc libc-test surface

SHELL=/bin/bash -o pipefail	

DIR = /tmp/libc
TAR = musl-7ada6dde6f9dc6a2836c3d92c2f762d35fd229e0.tar.gz
URL = https://git.musl-libc.org/cgit/musl/snapshot/$(TAR)

all: editor
	golint 2>&1
	staticcheck 2>&1

build_all_targets:
	./build_all_targets.sh
	echo done

clean:
	rm -f log-* cpu.test mem.test *.out
	git clean -fd
	find testdata/nsz.repo.hu/ -name \*.go -delete
	make -C testdata/nsz.repo.hu/libc-test/ cleanall
	go clean

check:
	staticcheck 2>&1 | grep -v U1000

download:
	@if [ ! -f $(TAR) ]; then wget $(URL) ; fi

edit:
	@touch log
	@if [ -f "Session.vim" ]; then novim -S & else novim -p Makefile go.mod builder.json & fi

editor:
	gofmt -l -s -w *.go 2>&1 | tee log-editor
	go test -c -o /dev/null 2>&1 | tee -a log-editor
	go install -v  2>&1 | tee -a log-editor
	go build -o /dev/null generator*.go

generate: download
	mkdir -p $(DIR) || true
	rm -rf $(DIR)/*
	GO_GENERATE_DIR=$(DIR) go run generator*.go 2>&1 | tee log-generate
	go build -v
	# go install github.com/mdempsky/unconvert@latest
	go build -v 2>&1 | tee -a log-generate
	go test -v -short -count=1 ./... | tee -a log-generate
	git status | tee -a log-generate
	grep 'TRC\|TODO\|ERRORF\|FAIL' log-generate || true

dev: download
	mkdir -p $(DIR) || true
	rm -rf $(DIR)/*
	echo -n > /tmp/ccgo.log
	GO_GENERATE_DIR=$(DIR) GO_GENERATE_DEV=1 go run -tags=ccgo.dmesg,ccgo.assert generator*.go 2>&1 | tee log-generate
	go build -v | tee -a log-generate
	go test -v -short -count=1 ./... | tee -a log-generate
	git status | tee -a log-generate
	grep 'TRC\|TODO\|ERRORF\|FAIL' log-generate || true
	grep 'TRC\|TODO\|ERRORF\|FAIL' /tmp/ccgo.log || true

membrk-test:
	echo -n > /tmp/ccgo.log
	touch log-test
	cp log-test log-test0
	go test -v -timeout 24h -count=1 -tags=libc.membrk 2>&1 | tee log-test
	grep -a 'TRC\|TODO\|ERRORF\|FAIL' log-test || true 2>&1 | tee -a log-test

test:
	echo -n > /tmp/ccgo.log
	touch log-test
	cp log-test log-test0
	go test -v -timeout 24h -count=1 2>&1 | tee log-test

short-test:
	echo -n > /tmp/ccgo.log
	touch log-test
	cp log-test log-test0
	go test -v -timeout 24h -count=1 -short 2>&1 | tee log-test
	grep -a 'TRC\|TODO\|ERRORF\|FAIL' log-test || true 2>&1 | tee -a log-test

xlibc:
	echo -n > /tmp/ccgo.log
	touch log-test
	cp log-test log-test0
	go test -v -timeout 24h -count=1 -tags=ccgo.dmesg,ccgo.assert 2>&1 -run TestLibc | tee log-test
	grep -a 'TRC\|TODO\|ERRORF\|FAIL' log-test || true 2>&1 | tee -a log-test

xpthread:
	echo -n > /tmp/ccgo.log
	touch log-test
	cp log-test log-test0
	go test -v -timeout 24h -count=1 2>&1 -run TestLibc -re pthread | tee log-test
	grep -a 'TRC\|TODO\|ERRORF\|FAIL' log-test || true 2>&1 | tee -a log-test

libc-test:
	echo -n > /tmp/ccgo.log
	touch log-test
	cp log-test log-test0
	go test -v -timeout 24h -count=1 2>&1 -run TestLibc | tee log-test
	# grep -a 'TRC\|TODO\|ERRORF\|FAIL' log-test || true 2>&1 | tee -a log-test
	grep -o 'undefined: \<.*\>' log-test | sort -u

xtest:
	echo -n > /tmp/ccgo.log
	touch log-test
	cp log-test log-test0
	go test -v -timeout 24h -count=1 -tags=ccgo.dmesg,ccgo.assert 2>&1 | tee log-test
	grep -a 'TRC\|TODO\|ERRORF\|FAIL' log-test || true 2>&1 | tee -a log-test

work:
	rm -f go.work*
	go work init
	go work use .
	go work use ../ccgo/v4
	go work use ../ccgo/v3
	go work use ../cc/v4

surface:
	surface > surface.new
	surface surface.old surface.new > log-todo-surface || true
