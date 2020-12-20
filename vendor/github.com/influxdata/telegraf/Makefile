next_version :=  $(shell cat build_version.txt)
tag := $(shell git describe --exact-match --tags 2>git_describe_error.tmp; rm -f git_describe_error.tmp)
branch := $(shell git rev-parse --abbrev-ref HEAD)
commit := $(shell git rev-parse --short=8 HEAD)
glibc_version := 2.17

ifdef NIGHTLY
	version := $(next_version)
	rpm_version := nightly
	rpm_iteration := 0
	deb_version := nightly
	deb_iteration := 0
	tar_version := nightly
else ifeq ($(tag),)
	version := $(next_version)
	rpm_version := $(version)~$(commit)-0
	rpm_iteration := 0
	deb_version := $(version)~$(commit)-0
	deb_iteration := 0
	tar_version := $(version)~$(commit)
else ifneq ($(findstring -rc,$(tag)),)
	version := $(word 1,$(subst -, ,$(tag)))
	version := $(version:v%=%)
	rc := $(word 2,$(subst -, ,$(tag)))
	rpm_version := $(version)-0.$(rc)
	rpm_iteration := 0.$(subst rc,,$(rc))
	deb_version := $(version)~$(rc)-1
	deb_iteration := 0
	tar_version := $(version)~$(rc)
else
	version := $(tag:v%=%)
	rpm_version := $(version)-1
	rpm_iteration := 1
	deb_version := $(version)-1
	deb_iteration := 1
	tar_version := $(version)
endif

MAKEFLAGS += --no-print-directory
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
HOSTGO := env -u GOOS -u GOARCH -u GOARM -- go

LDFLAGS := $(LDFLAGS) -X main.commit=$(commit) -X main.branch=$(branch) -X main.goos=$(GOOS) -X main.goarch=$(GOARCH)
ifneq ($(tag),)
	LDFLAGS += -X main.version=$(version)
endif

# Go built-in race detector works only for 64 bits architectures.
ifneq ($(GOARCH), 386)
	race_detector := -race
endif


GOFILES ?= $(shell git ls-files '*.go')
GOFMT ?= $(shell gofmt -l -s $(filter-out plugins/parsers/influx/machine.go, $(GOFILES)))

prefix ?= /usr/local
bindir ?= $(prefix)/bin
sysconfdir ?= $(prefix)/etc
localstatedir ?= $(prefix)/var
pkgdir ?= build/dist

.PHONY: all
all:
	@$(MAKE) deps
	@$(MAKE) telegraf

.PHONY: help
help:
	@echo 'Targets:'
	@echo '  all        - download dependencies and compile telegraf binary'
	@echo '  deps       - download dependencies'
	@echo '  telegraf   - compile telegraf binary'
	@echo '  test       - run short unit tests'
	@echo '  fmt        - format source files'
	@echo '  tidy       - tidy go modules'
	@echo '  check-deps - check docs/LICENSE_OF_DEPENDENCIES.md'
	@echo '  clean      - delete build artifacts'
	@echo ''
	@echo 'Package Targets:'
	@$(foreach dist,$(dists),echo "  $(dist)";)

.PHONY: deps
deps:
	go mod download

.PHONY: telegraf
telegraf:
	go build -ldflags "$(LDFLAGS)" ./cmd/telegraf

# Used by dockerfile builds
.PHONY: go-install
go-install:
	go install -mod=mod -ldflags "-w -s $(LDFLAGS)" ./cmd/telegraf

.PHONY: test
test:
	go test -short $(race_detector) ./...

.PHONY: fmt
fmt:
	@gofmt -s -w $(filter-out plugins/parsers/influx/machine.go, $(GOFILES))

.PHONY: fmtcheck
fmtcheck:
	@if [ ! -z "$(GOFMT)" ]; then \
		echo "[ERROR] gofmt has found errors in the following files:"  ; \
		echo "$(GOFMT)" ; \
		echo "" ;\
		echo "Run make fmt to fix them." ; \
		exit 1 ;\
	fi

.PHONY: test-windows
test-windows:
	go test -short $(race_detector) ./plugins/inputs/ping/...
	go test -short $(race_detector) ./plugins/inputs/win_perf_counters/...
	go test -short $(race_detector) ./plugins/inputs/win_services/...
	go test -short $(race_detector) ./plugins/inputs/procstat/...
	go test -short $(race_detector) ./plugins/inputs/ntpq/...
	go test -short $(race_detector) ./plugins/processors/port_name/...

.PHONY: vet
vet:
	@echo 'go vet $$(go list ./... | grep -v ./plugins/parsers/influx)'
	@go vet $$(go list ./... | grep -v ./plugins/parsers/influx) ; if [ $$? -ne 0 ]; then \
		echo ""; \
		echo "go vet has found suspicious constructs. Please remediate any reported errors"; \
		echo "to fix them before submitting code for review."; \
		exit 1; \
	fi

.PHONY: tidy
tidy:
	go mod verify
	go mod tidy
	@if ! git diff --quiet go.mod go.sum; then \
		echo "please run go mod tidy and check in changes"; \
		exit 1; \
	fi

.PHONY: check
check: fmtcheck vet
	@$(MAKE) --no-print-directory tidy

.PHONY: test-all
test-all: fmtcheck vet
	go test $(race_detector) ./...

.PHONY: check-deps
check-deps:
	./scripts/check-deps.sh

.PHONY: clean
clean:
	rm -f telegraf
	rm -f telegraf.exe
	rm -rf build

.PHONY: docker-image
docker-image:
	docker build -f scripts/buster.docker -t "telegraf:$(commit)" .

plugins/parsers/influx/machine.go: plugins/parsers/influx/machine.go.rl
	ragel -Z -G2 $^ -o $@

.PHONY: plugin-%
plugin-%:
	@echo "Starting dev environment for $${$(@)} input plugin..."
	@docker-compose -f plugins/inputs/$${$(@)}/dev/docker-compose.yml up

.PHONY: ci-1.15
ci-1.15:
	docker build -t quay.io/influxdb/telegraf-ci:1.15.2 - < scripts/ci-1.15.docker
	docker push quay.io/influxdb/telegraf-ci:1.15.2

.PHONY: ci-1.14
ci-1.14:
	docker build -t quay.io/influxdb/telegraf-ci:1.14.9 - < scripts/ci-1.14.docker
	docker push quay.io/influxdb/telegraf-ci:1.14.9

.PHONY: install
install: $(buildbin)
	@mkdir -pv $(DESTDIR)$(bindir)
	@mkdir -pv $(DESTDIR)$(sysconfdir)
	@mkdir -pv $(DESTDIR)$(localstatedir)
	@if [ $(GOOS) != "windows" ]; then mkdir -pv $(DESTDIR)$(sysconfdir)/logrotate.d; fi
	@if [ $(GOOS) != "windows" ]; then mkdir -pv $(DESTDIR)$(localstatedir)/log/telegraf; fi
	@if [ $(GOOS) != "windows" ]; then mkdir -pv $(DESTDIR)$(sysconfdir)/telegraf/telegraf.d; fi
	@cp -fv $(buildbin) $(DESTDIR)$(bindir)
	@if [ $(GOOS) != "windows" ]; then cp -fv etc/telegraf.conf $(DESTDIR)$(sysconfdir)/telegraf/telegraf.conf$(conf_suffix); fi
	@if [ $(GOOS) != "windows" ]; then cp -fv etc/logrotate.d/telegraf $(DESTDIR)$(sysconfdir)/logrotate.d; fi
	@if [ $(GOOS) = "windows" ]; then cp -fv etc/telegraf_windows.conf $(DESTDIR)/telegraf.conf; fi
	@if [ $(GOOS) = "linux" ]; then scripts/check-dynamic-glibc-versions.sh $(buildbin) $(glibc_version); fi
	@if [ $(GOOS) = "linux" ]; then mkdir -pv $(DESTDIR)$(prefix)/lib/telegraf/scripts; fi
	@if [ $(GOOS) = "linux" ]; then cp -fv scripts/telegraf.service $(DESTDIR)$(prefix)/lib/telegraf/scripts; fi
	@if [ $(GOOS) = "linux" ]; then cp -fv scripts/init.sh $(DESTDIR)$(prefix)/lib/telegraf/scripts; fi

# Telegraf build per platform.  This improves package performance by sharing
# the bin between deb/rpm/tar packages over building directly into the package
# directory.
$(buildbin):
	@mkdir -pv $(dir $@)
	go build -o $(dir $@) -ldflags "$(LDFLAGS)" ./cmd/telegraf

debs := telegraf_$(deb_version)_amd64.deb
debs += telegraf_$(deb_version)_arm64.deb
debs += telegraf_$(deb_version)_armel.deb
debs += telegraf_$(deb_version)_armhf.deb
debs += telegraf_$(deb_version)_i386.deb
debs += telegraf_$(deb_version)_mips.deb
debs += telegraf_$(deb_version)_mipsel.deb
debs += telegraf_$(deb_version)_s390x.deb

rpms += telegraf-$(rpm_version).aarch64.rpm
rpms += telegraf-$(rpm_version).armel.rpm
rpms += telegraf-$(rpm_version).armv6hl.rpm
rpms += telegraf-$(rpm_version).i386.rpm
rpms += telegraf-$(rpm_version).s390x.rpm
rpms += telegraf-$(rpm_version).x86_64.rpm

tars += telegraf-$(tar_version)_darwin_amd64.tar.gz
tars += telegraf-$(tar_version)_freebsd_amd64.tar.gz
tars += telegraf-$(tar_version)_freebsd_i386.tar.gz
tars += telegraf-$(tar_version)_linux_amd64.tar.gz
tars += telegraf-$(tar_version)_linux_arm64.tar.gz
tars += telegraf-$(tar_version)_linux_armel.tar.gz
tars += telegraf-$(tar_version)_linux_armhf.tar.gz
tars += telegraf-$(tar_version)_linux_i386.tar.gz
tars += telegraf-$(tar_version)_linux_mips.tar.gz
tars += telegraf-$(tar_version)_linux_mipsel.tar.gz
tars += telegraf-$(tar_version)_linux_s390x.tar.gz
tars += telegraf-$(tar_version)_static_linux_amd64.tar.gz

zips += telegraf-$(tar_version)_windows_amd64.zip
zips += telegraf-$(tar_version)_windows_i386.zip

dists := $(debs) $(rpms) $(tars) $(zips)

.PHONY: package
package: $(dists)

rpm_amd64 := amd64
rpm_386 := i386
rpm_s390x := s390x
rpm_arm5 := armel
rpm_arm6 := armv6hl
rpm_arm647 := aarch64
rpm_arch = $(rpm_$(GOARCH)$(GOARM))

.PHONY: $(rpms)
$(rpms):
	@$(MAKE) install
	@mkdir -p $(pkgdir)
	fpm --force \
		--log info \
		--architecture $(rpm_arch) \
		--input-type dir \
		--output-type rpm \
		--vendor InfluxData \
		--url https://github.com/influxdata/telegraf \
		--license MIT \
		--maintainer support@influxdb.com \
		--config-files /etc/telegraf/telegraf.conf \
		--config-files /etc/logrotate.d/telegraf \
		--after-install scripts/rpm/post-install.sh \
		--before-install scripts/rpm/pre-install.sh \
		--after-remove scripts/rpm/post-remove.sh \
		--description "Plugin-driven server agent for reporting metrics into InfluxDB." \
		--depends coreutils \
		--depends shadow-utils \
		--rpm-posttrans scripts/rpm/post-install.sh \
		--name telegraf \
		--version $(version) \
		--iteration $(rpm_iteration) \
        --chdir $(DESTDIR) \
		--package $(pkgdir)/$@

deb_amd64 := amd64
deb_386 := i386
deb_s390x := s390x
deb_arm5 := armel
deb_arm6 := armhf
deb_arm647 := arm64
deb_mips := mips
deb_mipsle := mipsel
deb_arch = $(deb_$(GOARCH)$(GOARM))

.PHONY: $(debs)
$(debs):
	@$(MAKE) install
	@mkdir -pv $(pkgdir)
	fpm --force \
		--log info \
		--architecture $(deb_arch) \
		--input-type dir \
		--output-type deb \
		--vendor InfluxData \
		--url https://github.com/influxdata/telegraf \
		--license MIT \
		--maintainer support@influxdb.com \
		--config-files /etc/telegraf/telegraf.conf.sample \
		--config-files /etc/logrotate.d/telegraf \
		--after-install scripts/deb/post-install.sh \
		--before-install scripts/deb/pre-install.sh \
		--after-remove scripts/deb/post-remove.sh \
		--before-remove scripts/deb/pre-remove.sh \
		--description "Plugin-driven server agent for reporting metrics into InfluxDB." \
		--name telegraf \
		--version $(version) \
		--iteration $(deb_iteration) \
		--chdir $(DESTDIR) \
		--package $(pkgdir)/$@

.PHONY: $(zips)
$(zips):
	@$(MAKE) install
	@mkdir -p $(pkgdir)
	(cd $(dir $(DESTDIR)) && zip -r - ./*) > $(pkgdir)/$@

.PHONY: $(tars)
$(tars):
	@$(MAKE) install
	@mkdir -p $(pkgdir)
	tar --owner 0 --group 0 -czvf $(pkgdir)/$@ -C $(dir $(DESTDIR)) .

.PHONY: upload-nightly
upload-nightly:
	aws s3 sync $(pkgdir) s3://dl.influxdata.com/telegraf/nightlies/ \
		--exclude "*" \
		--include "*.tar.gz" \
		--include "*.deb" \
		--include "*.rpm" \
		--include "*.zip" \
		--acl public-read

%amd64.deb %x86_64.rpm %linux_amd64.tar.gz: export GOOS := linux
%amd64.deb %x86_64.rpm %linux_amd64.tar.gz: export GOARCH := amd64

%static_linux_amd64.tar.gz: export cgo := -nocgo
%static_linux_amd64.tar.gz: export CGO_ENABLED := 0

%i386.deb %i386.rpm %linux_i386.tar.gz: export GOOS := linux
%i386.deb %i386.rpm %linux_i386.tar.gz: export GOARCH := 386

%armel.deb %armel.rpm %linux_armel.tar.gz: export GOOS := linux
%armel.deb %armel.rpm %linux_armel.tar.gz: export GOARCH := arm
%armel.deb %armel.rpm %linux_armel.tar.gz: export GOARM := 5

%armhf.deb %armv6hl.rpm %linux_armhf.tar.gz: export GOOS := linux
%armhf.deb %armv6hl.rpm %linux_armhf.tar.gz: export GOARCH := arm
%armhf.deb %armv6hl.rpm %linux_armhf.tar.gz: export GOARM := 6

%arm64.deb %aarch64.rpm %linux_arm64.tar.gz: export GOOS := linux
%arm64.deb %aarch64.rpm %linux_arm64.tar.gz: export GOARCH := arm64
%arm64.deb %aarch64.rpm %linux_arm64.tar.gz: export GOARM := 7

%mips.deb %linux_mips.tar.gz: export GOOS := linux
%mips.deb %linux_mips.tar.gz: export GOARCH := mips

%mipsel.deb %linux_mipsel.tar.gz: export GOOS := linux
%mipsel.deb %linux_mipsel.tar.gz: export GOARCH := mipsle

%s390x.deb %s390x.rpm %linux_s390x.tar.gz: export GOOS := linux
%s390x.deb %s390x.rpm %linux_s390x.tar.gz: export GOARCH := s390x

%freebsd_amd64.tar.gz: export GOOS := freebsd
%freebsd_amd64.tar.gz: export GOARCH := amd64

%freebsd_i386.tar.gz: export GOOS := freebsd
%freebsd_i386.tar.gz: export GOARCH := 386

%windows_amd64.zip: export GOOS := windows
%windows_amd64.zip: export GOARCH := amd64

%darwin_amd64.tar.gz: export GOOS := darwin
%darwin_amd64.tar.gz: export GOARCH := amd64

%windows_i386.zip: export GOOS := windows
%windows_i386.zip: export GOARCH := 386

%windows_i386.zip %windows_amd64.zip: export prefix =
%windows_i386.zip %windows_amd64.zip: export bindir = $(prefix)
%windows_i386.zip %windows_amd64.zip: export sysconfdir = $(prefix)
%windows_i386.zip %windows_amd64.zip: export localstatedir = $(prefix)
%windows_i386.zip %windows_amd64.zip: export EXEEXT := .exe

%.deb: export pkg := deb
%.deb: export prefix := /usr
%.deb: export conf_suffix := .sample
%.deb: export sysconfdir := /etc
%.deb: export localstatedir := /var
%.rpm: export pkg := rpm
%.rpm: export prefix := /usr
%.rpm: export sysconfdir := /etc
%.rpm: export localstatedir := /var
%.tar.gz: export pkg := tar
%.tar.gz: export prefix := /usr
%.tar.gz: export sysconfdir := /etc
%.tar.gz: export localstatedir := /var
%.zip: export pkg := zip
%.zip: export prefix := /

%.deb %.rpm %.tar.gz %.zip: export DESTDIR = build/$(GOOS)-$(GOARCH)$(GOARM)$(cgo)-$(pkg)/telegraf-$(version)
%.deb %.rpm %.tar.gz %.zip: export buildbin = build/$(GOOS)-$(GOARCH)$(GOARM)$(cgo)/telegraf$(EXEEXT)
%.deb %.rpm %.tar.gz %.zip: export LDFLAGS = -w -s
