// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package internal

// Shared gRPC routing header names. Exported so both the classic
// bigtable package and the internal/session package can reach them
// without either package duplicating string literals or importing the
// other. The top-level bigtable package keeps its own copies today for
// back-compat; new callers should use these.
const (
	ResourcePrefixHeader = "google-cloud-resource-prefix"
	RequestParamsHeader  = "x-goog-request-params"
)
