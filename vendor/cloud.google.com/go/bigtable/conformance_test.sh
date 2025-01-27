#!/bin/bash

# Copyright 2023 Google LLC
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

# Fail on any error
set -eo pipefail

# Display commands being run
set -x

## cd to the parent directory, i.e. the root of the git repo
cd $(dirname $0)/../..

rootDir=$(pwd)
clientLibHome=$rootDir/google-cloud-go/bigtable
testProxyHome=$clientLibHome/internal/testproxy
testProxyPort=9999
conformanceTestsHome=$rootDir/cloud-bigtable-clients-test/tests

sponge_log=$clientLibHome/sponge_log.log

cd $testProxyHome
GOWORK=off go build

nohup $testProxyHome/testproxy --port $testProxyPort &
proxyPID=$!

# Stop the testproxy
function cleanup() {
    echo "Cleanup testproxy and move logs"
    kill -9 $proxyPID
    # Takes the kokoro output log (raw stdout) and creates a machine-parseable
    # xUnit XML file.
    cat $sponge_log |
      go-junit-report -set-exit-code >$clientLibHome/sponge_log.xml
}
trap cleanup EXIT

# Run the conformance tests
cd $conformanceTestsHome
# Tests in https://github.com/googleapis/cloud-bigtable-clients-test/tree/main/tests can only be run on go1.22.7
go install golang.org/dl/go1.22.7@latest
go1.22.7 download
# known_failures.txt contains tests for the unimplemented features
eval "go1.22.7 test -v -proxy_addr=:$testProxyPort -skip `tr -d '\n' < $testProxyHome/known_failures.txt` | tee -a $sponge_log"
RETURN_CODE=$?

echo "exiting with ${RETURN_CODE}"
exit ${RETURN_CODE}
