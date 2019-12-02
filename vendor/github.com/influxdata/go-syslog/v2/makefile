SHELL := /bin/bash
RAGEL := ragel -I common

export GO_TEST=env GOTRACEBACK=all GO111MODULE=on go test $(GO_ARGS)

.PHONY: build
build: rfc5424/machine.go rfc5424/builder.go nontransparent/parser.go
	@gofmt -w -s ./rfc5424
	@gofmt -w -s ./octetcounting
	@gofmt -w -s ./nontransparent

rfc5424/machine.go: rfc5424/machine.go.rl common/common.rl

rfc5424/builder.go: rfc5424/builder.go.rl common/common.rl

rfc5424/builder.go rfc5424/machine.go:
	$(RAGEL) -Z -G2 -e -o $@ $<
	@sed -i '/^\/\/line/d' $@
	$(MAKE) file=$@ snake2camel

nontransparent/parser.go: nontransparent/parser.go.rl
	$(RAGEL) -Z -G2 -e -o $@ $<
	@sed -i '/^\/\/line/d' $@
	$(MAKE) file=$@ snake2camel

.PHONY: snake2camel
snake2camel:
	@awk -i inplace '{ \
	while ( match($$0, /(.*)([a-z]+[0-9]*)_([a-zA-Z0-9])(.*)/, cap) ) \
	$$0 = cap[1] cap[2] toupper(cap[3]) cap[4]; \
	print \
	}' $(file)

.PHONY: bench
bench: rfc5424/*_test.go rfc5424/machine.go
	go test -bench=. -benchmem -benchtime=5s ./...

.PHONY: tests
tests:
	$(GO_TEST) ./...

docs/nontransparent.dot: nontransparent/parser.go.rl
	$(RAGEL) -Z -Vp $< -o $@

docs/rfc5424.dot: rfc5424/machine.go.rl common/common.rl
	$(RAGEL) -Z -Vp $< -o $@

docs/rfc5424_pri.dot: rfc5424/machine.go.rl common/common.rl
	$(RAGEL) -Z -Vp -M pri $< -o $@

docs/rfc5424_pri.png: docs/rfc5424_pri.dot
	dot $< -Tpng -o $@

docs/rfc5424_version.dot: rfc5424/machine.go.rl common/common.rl
	$(RAGEL) -Z -Vp -M version $< -o $@

docs/rfc5424_version.png: docs/rfc5424_version.dot
	dot $< -Tpng -o $@

docs/rfc5424_timestamp.dot: rfc5424/machine.go.rl common/common.rl
	$(RAGEL) -Z -Vp -M timestamp $< -o $@

docs/rfc5424_timestamp.png: docs/rfc5424_timestamp.dot
	dot $< -Tpng -o $@

docs/rfc5424_hostname.dot: rfc5424/machine.go.rl common/common.rl
	$(RAGEL) -Z -Vp -M hostname $< -o $@

docs/rfc5424_hostname.png: docs/rfc5424_hostname.dot
	dot $< -Tpng -o $@

docs/rfc5424_appname.dot: rfc5424/machine.go.rl common/common.rl
	$(RAGEL) -Z -Vp -M appname $< -o $@

docs/rfc5424_appname.png: docs/rfc5424_appname.dot
	dot $< -Tpng -o $@

docs/rfc5424_procid.dot: rfc5424/machine.go.rl common/common.rl
	$(RAGEL) -Z -Vp -M procid $< -o $@

docs/rfc5424_procid.png: docs/rfc5424_procid.dot
	dot $< -Tpng -o $@

docs/rfc5424_msgid.dot: rfc5424/machine.go.rl common/common.rl
	$(RAGEL) -Z -Vp -M msgid $< -o $@

docs/rfc5424_msgid.png: docs/rfc5424_msgid.dot
	dot $< -Tpng -o $@

docs/rfc5424_structureddata.dot: rfc5424/machine.go.rl common/common.rl
	$(RAGEL) -Z -Vp -M structureddata $< -o $@

docs/rfc5424_structureddata.png: docs/rfc5424_structureddata.dot
	dot $< -Tpng -o $@

docs/rfc5424_msg.dot: rfc5424/machine.go.rl common/common.rl
	$(RAGEL) -Z -Vp -M msg $< -o $@

docs/rfc5424_msg.png: docs/rfc5424_msg.dot
	dot $< -Tpng -o $@

docs:
	@mkdir -p docs

.PHONY: dots
dots: docs
	$(MAKE) -s docs/rfc5424.dot docs/nontransparent.dot docs/rfc5424_pri.dot docs/rfc5424_version.dot docs/rfc5424_timestamp.dot docs/rfc5424_hostname.dot docs/rfc5424_appname.dot docs/rfc5424_procid.dot docs/rfc5424_msgid.dot docs/rfc5424_structureddata.dot docs/rfc5424_msg.dot

.PHONY: graph
graph: dots docs/rfc5424_pri.png docs/rfc5424_version.png docs/rfc5424_timestamp.png docs/rfc5424_hostname.png docs/rfc5424_appname.png docs/rfc5424_procid.png docs/rfc5424_msgid.png docs/rfc5424_structureddata.png docs/rfc5424_msg.png

.PHONY: clean
clean: rfc5424/machine.go nontransparent/parser.go
	@rm -f $?
	@rm -rf docs
