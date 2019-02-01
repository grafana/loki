#!/bin/sh
#
# Copyright 2016 Google Inc. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

go get github.com/golang/protobuf/protoc-gen-go

protoc \
--go_out=Mgoogle/protobuf/any.proto=github.com/golang/protobuf/ptypes/any:. \
OpenAPIv2/OpenAPIv2.proto 

protoc \
-I.:$GOPATH/src \
--go_out=:. \
plugins/plugin.proto 

protoc \
--go_out=Mgoogle/protobuf/any.proto=github.com/golang/protobuf/ptypes/any:. \
OpenAPIv3/OpenAPIv3.proto 

protoc \
--go_out=Mgoogle/protobuf/any.proto=github.com/golang/protobuf/ptypes/any:. \
discovery/discovery.proto 
