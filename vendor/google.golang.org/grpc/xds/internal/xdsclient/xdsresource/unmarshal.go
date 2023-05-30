/*
 *
 * Copyright 2021 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// Package xdsresource contains functions to proto xds updates (unmarshal from
// proto), and types for the resource updates.
package xdsresource

import (
	"errors"
	"fmt"
	"strings"

	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/protobuf/types/known/anypb"
)

// UnmarshalOptions wraps the input parameters for `UnmarshalXxx` functions.
type UnmarshalOptions struct {
	// Version is the version of the received response.
	Version string
	// Resources are the xDS resources resources in the received response.
	Resources []*anypb.Any
	// Logger is the prefix logger to be used during unmarshaling.
	Logger *grpclog.PrefixLogger
	// UpdateValidator is a post unmarshal validation check provided by the
	// upper layer.
	UpdateValidator UpdateValidatorFunc
}

// CombineErrors TBD.
func CombineErrors(rType string, topLevelErrors []error, perResourceErrors map[string]error) error {
	var errStrB strings.Builder
	errStrB.WriteString(fmt.Sprintf("error parsing %q response: ", rType))
	if len(topLevelErrors) > 0 {
		errStrB.WriteString("top level errors: ")
		for i, err := range topLevelErrors {
			if i != 0 {
				errStrB.WriteString(";\n")
			}
			errStrB.WriteString(err.Error())
		}
	}
	if len(perResourceErrors) > 0 {
		var i int
		for name, err := range perResourceErrors {
			if i != 0 {
				errStrB.WriteString(";\n")
			}
			i++
			errStrB.WriteString(fmt.Sprintf("resource %q: %v", name, err.Error()))
		}
	}
	return errors.New(errStrB.String())
}
