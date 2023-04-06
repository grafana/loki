// Copyright 2012-2018 The NATS Authors
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

//go:build !windows
// +build !windows

package server

// Run starts the NATS server. This wrapper function allows Windows to add a
// hook for running NATS as a service.
func Run(server *Server) error {
	server.Start()
	return nil
}

// isWindowsService indicates if NATS is running as a Windows service.
func isWindowsService() bool {
	return false
}
