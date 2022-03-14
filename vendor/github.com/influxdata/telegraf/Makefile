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
else
	LDFLAGS += -X main.version=$(version)-$(commit)
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
	@echo '  all          - download dependencies and compile telegraf binary'
	@echo '  deps         - download dependencies'
	@echo '  telegraf     - compile telegraf binary'
	@echo '  test         - run short unit tests'
	@echo '  fmt          - format source files'
	@echo '  tidy         - tidy go modules'
	@echo '  lint         - run linter'
	@echo '  lint-branch  - run linter on changes in current branch since master'
	@echo '  lint-install - install linter'
	@echo '  check-deps   - check docs/LICENSE_OF_DEPENDENCIES.md'
	@echo '  clean        - delete build artifacts'
	@echo '  package      - build all supported packages, override include_packages to only build a subset'
	@echo '                 e.g.: make package include_packages="amd64.deb"'
	@echo ''
	@echo 'Possible values for include_packages variable'
	@$(foreach package,$(include_packages),echo "  $(package)";)
	@echo ''
	@echo 'Resulting package name format (where arch will be the arch of the package):'
	@echo '   telegraf_$(deb_version)_arch.deb'
	@echo '   telegraf-$(rpm_version).arch.rpm'
	@echo '   telegraf-$(tar_version)_arch.tar.gz'
	@echo '   telegraf-$(tar_version)_arch.zip'


.PHONY: deps
deps:
	go mod download -x

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

.PHONY: test-integration
test-integration:
	go test -run Integration $(race_detector) ./...

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

.PHONY: vet
vet:
	@echo 'go vet $$(go list ./... | grep -v ./plugins/parsers/influx)'
	@go vet $$(go list ./... | grep -v ./plugins/parsers/influx) ; if [ $$? -ne 0 ]; then \
		echo ""; \
		echo "go vet has found suspicious constructs. Please remediate any reported errors"; \
		echo "to fix them before submitting code for review."; \
		exit 1; \
	fi

.PHONY: lint-install
lint-install:
	@echo "Installing golangci-lint"
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.42.1

	@echo "Installing markdownlint"
	npm install -g markdownlint-cli

.PHONY: lint
lint:
	@which golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint not found, please run: make lint-install"; \
		exit 1; \
	}
	golangci-lint run

	@which markdownlint >/dev/null 2>&1 || { \
		echo "markdownlint not found, please run: make lint-install"; \
		exit 1; \
	}
	markdownlint .

.PHONY: lint-branch
lint-branch:
	@which golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint not found, please run: make lint-install"; \
		exit 1; \
	}

	golangci-lint run --new-from-rev master

.PHONY: tidy
tidy:
	go mod verify
	go mod tidy
	@if ! git diff --quiet go.mod go.sum; then \
		echo "please run go mod tidy and check in changes, you might have to use the same version of Go as the CI"; \
		exit 1; \
	fi

.PHONY: check
check: fmtcheck vet

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

.PHONY: ci-1.17
ci-1.17:
	docker build -t quay.io/influxdb/telegraf-ci:1.17.7 - < scripts/ci-1.17.docker
	docker push quay.io/influxdb/telegraf-ci:1.17.7

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

# Define packages Telegraf supports, organized by architecture with a rule to echo the list to limit include_packages
# e.g. make package include_packages="$(make amd64)"
mips += linux_mips.tar.gz mips.deb
.PHONY: mips
mips:
	@ echo $(mips)
mipsel += mipsel.deb linux_mipsel.tar.gz
.PHONY: mipsel
mipsel:
	@ echo $(mipsel)
arm64 += linux_arm64.tar.gz arm64.deb aarch64.rpm
.PHONY: arm64
arm64:
	@ echo $(arm64)
amd64 += freebsd_amd64.tar.gz linux_amd64.tar.gz amd64.deb x86_64.rpm
.PHONY: amd64
amd64:
	@ echo $(amd64)
static += static_linux_amd64.tar.gz
.PHONY: static
static:
	@ echo $(static)
armel += linux_armel.tar.gz armel.rpm armel.deb
.PHONY: armel
armel:
	@ echo $(armel)
armhf += linux_armhf.tar.gz freebsd_armv7.tar.gz armhf.deb armv6hl.rpm
.PHONY: armhf
armhf:
	@ echo $(armhf)
s390x += linux_s390x.tar.gz s390x.deb s390x.rpm
.PHONY: riscv64
riscv64:
	@ echo $(riscv64)
riscv64 += linux_riscv64.tar.gz riscv64.rpm riscv64.deb
.PHONY: s390x
s390x:
	@ echo $(s390x)
ppc64le += linux_ppc64le.tar.gz ppc64le.rpm ppc64el.deb
.PHONY: ppc64le
ppc64le:
	@ echo $(ppc64le)
i386 += freebsd_i386.tar.gz i386.deb linux_i386.tar.gz i386.rpm
.PHONY: i386
i386:
	@ echo $(i386)
windows += windows_i386.zip windows_amd64.zip
.PHONY: windows
windows:
	@ echo $(windows)
darwin-amd64 += darwin_amd64.tar.gz
.PHONY: darwin-amd64
darwin-amd64:
	@ echo $(darwin-amd64)

darwin-arm64 += darwin_arm64.tar.gz
.PHONY: darwin-arm64
darwin-arm64:
	@ echo $(darwin-arm64)

include_packages := $(mips) $(mipsel) $(arm64) $(amd64) $(static) $(armel) $(armhf) $(riscv64) $(s390x) $(ppc64le) $(i386) $(windows) $(darwin-amd64) $(darwin-arm64)

.PHONY: package
package: $(include_packages)

.PHONY: $(include_packages)
$(include_packages):
	@$(MAKE) install
	@mkdir -p $(pkgdir)

	@if [ "$(suffix $@)" = ".rpm" ]; then \
		fpm --force \
			--log info \
			--architecture $(basename $@) \
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
			--rpm-digest sha256 \
			--rpm-posttrans scripts/rpm/post-install.sh \
			--name telegraf \
			--version $(version) \
			--iteration $(rpm_iteration) \
			--chdir $(DESTDIR) \
			--package $(pkgdir)/telegraf-$(rpm_version).$@ ;\
	elif [ "$(suffix $@)" = ".deb" ]; then \
		fpm --force \
			--log info \
			--architecture $(basename $@) \
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
			--package $(pkgdir)/telegraf_$(deb_version)_$@	;\
	elif [ "$(suffix $@)" = ".zip" ]; then \
		(cd $(dir $(DESTDIR)) && zip -r - ./*) > $(pkgdir)/telegraf-$(tar_version)_$@ ;\
	elif [ "$(suffix $@)" = ".gz" ]; then \
		tar --owner 0 --group 0 -czvf $(pkgdir)/telegraf-$(tar_version)_$@ -C $(dir $(DESTDIR)) . ;\
	fi

amd64.deb x86_64.rpm linux_amd64.tar.gz: export GOOS := linux
amd64.deb x86_64.rpm linux_amd64.tar.gz: export GOARCH := amd64

static_linux_amd64.tar.gz: export cgo := -nocgo
static_linux_amd64.tar.gz: export CGO_ENABLED := 0

i386.deb i386.rpm linux_i386.tar.gz: export GOOS := linux
i386.deb i386.rpm linux_i386.tar.gz: export GOARCH := 386

armel.deb armel.rpm linux_armel.tar.gz: export GOOS := linux
armel.deb armel.rpm linux_armel.tar.gz: export GOARCH := arm
armel.deb armel.rpm linux_armel.tar.gz: export GOARM := 5

armhf.deb armv6hl.rpm linux_armhf.tar.gz: export GOOS := linux
armhf.deb armv6hl.rpm linux_armhf.tar.gz: export GOARCH := arm
armhf.deb armv6hl.rpm linux_armhf.tar.gz: export GOARM := 6

arm64.deb aarch64.rpm linux_arm64.tar.gz: export GOOS := linux
arm64.deb aarch64.rpm linux_arm64.tar.gz: export GOARCH := arm64
arm64.deb aarch64.rpm linux_arm64.tar.gz: export GOARM := 7

mips.deb linux_mips.tar.gz: export GOOS := linux
mips.deb linux_mips.tar.gz: export GOARCH := mips

mipsel.deb linux_mipsel.tar.gz: export GOOS := linux
mipsel.deb linux_mipsel.tar.gz: export GOARCH := mipsle

riscv64.deb riscv64.rpm linux_riscv64.tar.gz: export GOOS := linux
riscv64.deb riscv64.rpm linux_riscv64.tar.gz: export GOARCH := riscv64

s390x.deb s390x.rpm linux_s390x.tar.gz: export GOOS := linux
s390x.deb s390x.rpm linux_s390x.tar.gz: export GOARCH := s390x

ppc64el.deb ppc64le.rpm linux_ppc64le.tar.gz: export GOOS := linux
ppc64el.deb ppc64le.rpm linux_ppc64le.tar.gz: export GOARCH := ppc64le

freebsd_amd64.tar.gz: export GOOS := freebsd
freebsd_amd64.tar.gz: export GOARCH := amd64

freebsd_i386.tar.gz: export GOOS := freebsd
freebsd_i386.tar.gz: export GOARCH := 386

freebsd_armv7.tar.gz: export GOOS := freebsd
freebsd_armv7.tar.gz: export GOARCH := arm
freebsd_armv7.tar.gz: export GOARM := 7

windows_amd64.zip: export GOOS := windows
windows_amd64.zip: export GOARCH := amd64

darwin_amd64.tar.gz: export GOOS := darwin
darwin_amd64.tar.gz: export GOARCH := amd64

darwin_arm64.tar.gz: export GOOS := darwin
darwin_arm64.tar.gz: export GOARCH := arm64

windows_i386.zip: export GOOS := windows
windows_i386.zip: export GOARCH := 386

windows_i386.zip windows_amd64.zip: export prefix =
windows_i386.zip windows_amd64.zip: export bindir = $(prefix)
windows_i386.zip windows_amd64.zip: export sysconfdir = $(prefix)
windows_i386.zip windows_amd64.zip: export localstatedir = $(prefix)
windows_i386.zip windows_amd64.zip: export EXEEXT := .exe

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
