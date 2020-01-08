#!/bin/bash
# Copyright 2019 Google Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Run at repo root.

go mod tidy
for i in `find . -name go.mod`; do
pushd `dirname $i`;
    # Update genproto and api to latest for every module (latest version is
    # always correct version). tidy will remove the dependencies if they're not
    # actually used by the client.
    go get -u google.golang.org/api
    go get -u google.golang.org/genproto
    go mod tidy;
popd;
done
