// Copyright 2020 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build gofuzz
// +build gofuzz

package server

var defaultFuzzServerOptions = Options{
	Host:                  "127.0.0.1",
	Trace:                 true,
	Debug:                 true,
	DisableShortFirstPing: true,
	NoLog:                 true,
	NoSigs:                true,
}

func dummyFuzzClient() *client {
	return &client{srv: New(&defaultFuzzServerOptions), msubs: -1, mpay: MAX_PAYLOAD_SIZE, mcl: MAX_CONTROL_LINE_SIZE}
}

func FuzzClient(data []byte) int {
	if len(data) < 100 {
		return -1
	}
	c := dummyFuzzClient()

	err := c.parse(data[:50])
	if err != nil {
		return 0
	}

	err = c.parse(data[50:])
	if err != nil {
		return 0
	}
	return 1
}
