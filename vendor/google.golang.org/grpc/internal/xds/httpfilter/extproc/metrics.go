/*
 *
 * Copyright 2026 gRPC authors.
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
 *
 */

package extproc

import (
	estats "google.golang.org/grpc/experimental/stats"
)

var (
	clientHeadersDurationMetric = estats.RegisterFloat64Histo(estats.MetricDescriptor{
		Name:        "grpc.client_ext_proc.client_headers_duration",
		Description: "Time between when the ext_proc filter sees the client's headers and when it allows those headers to continue on to the next filter.",
		Unit:        "s",
		Labels:      []string{"grpc.target"},
		Default:     false,
	})
	clientHalfCloseDurationMetric = estats.RegisterFloat64Histo(estats.MetricDescriptor{
		Name:        "grpc.client_ext_proc.client_half_close_duration",
		Description: "Time between when the ext_proc filter sees the client's half-close and when it allows that half-close to continue on to the next filter.",
		Unit:        "s",
		Labels:      []string{"grpc.target"},
		Default:     false,
	})
	serverHeadersDurationMetric = estats.RegisterFloat64Histo(estats.MetricDescriptor{
		Name:        "grpc.client_ext_proc.server_headers_duration",
		Description: "Time between when the ext_proc filter sees the server's headers and when it allows those headers to continue on to the next filter.",
		Unit:        "s",
		Labels:      []string{"grpc.target"},
		Default:     false,
	})
	serverTrailersDurationMetric = estats.RegisterFloat64Histo(estats.MetricDescriptor{
		Name:        "grpc.client_ext_proc.server_trailers_duration",
		Description: "Time between when the ext_proc filter sees the server's trailers and when it allows those trailers to continue on to the next filter.",
		Unit:        "s",
		Labels:      []string{"grpc.target"},
		Default:     false,
	})
)
