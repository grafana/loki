# Copyright 2018 The Prometheus Authors
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

include Makefile.common

.PHONY: deps
deps:
	@echo ">> getting dependencies"
	GO111MODULE=$(GO111MODULE) $(GO) get $(GOOPTS) -t ./...

.PHONY: test
test:: deps check_license unused common-test

ifeq (,$(shell echo $(GO_VERSION_NUMBER) | grep -E 'go1\.(7|8|9)\.'))
test:: staticcheck
endif
