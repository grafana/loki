// Copyright 2012-2019 The NATS Authors
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

package server

import (
	"os"
	"time"

	"golang.org/x/sys/windows/svc"
)

const (
	reopenLogCode   = 128
	reopenLogCmd    = svc.Cmd(reopenLogCode)
	ldmCode         = 129
	ldmCmd          = svc.Cmd(ldmCode)
	acceptReopenLog = svc.Accepted(reopenLogCode)
)

var serviceName = "nats-server"

// SetServiceName allows setting a different service name
func SetServiceName(name string) {
	serviceName = name
}

// winServiceWrapper implements the svc.Handler interface for implementing
// nats-server as a Windows service.
type winServiceWrapper struct {
	server *Server
}

var dockerized = false

func init() {
	if v, exists := os.LookupEnv("NATS_DOCKERIZED"); exists && v == "1" {
		dockerized = true
	}
}

// Execute will be called by the package code at the start of
// the service, and the service will exit once Execute completes.
// Inside Execute you must read service change requests from r and
// act accordingly. You must keep service control manager up to date
// about state of your service by writing into s as required.
// args contains service name followed by argument strings passed
// to the service.
// You can provide service exit code in exitCode return parameter,
// with 0 being "no error". You can also indicate if exit code,
// if any, is service specific or not by using svcSpecificEC
// parameter.
func (w *winServiceWrapper) Execute(args []string, changes <-chan svc.ChangeRequest,
	status chan<- svc.Status) (bool, uint32) {

	status <- svc.Status{State: svc.StartPending}
	go w.server.Start()

	// Wait for accept loop(s) to be started
	if !w.server.ReadyForConnections(10 * time.Second) {
		// Failed to start.
		return false, 1
	}

	status <- svc.Status{
		State:   svc.Running,
		Accepts: svc.AcceptStop | svc.AcceptShutdown | svc.AcceptParamChange | acceptReopenLog,
	}

loop:
	for change := range changes {
		switch change.Cmd {
		case svc.Interrogate:
			status <- change.CurrentStatus
		case svc.Stop, svc.Shutdown:
			w.server.Shutdown()
			break loop
		case reopenLogCmd:
			// File log re-open for rotating file logs.
			w.server.ReOpenLogFile()
		case ldmCmd:
			go w.server.lameDuckMode()
		case svc.ParamChange:
			if err := w.server.Reload(); err != nil {
				w.server.Errorf("Failed to reload server configuration: %s", err)
			}
		default:
			w.server.Debugf("Unexpected control request: %v", change.Cmd)
		}
	}

	status <- svc.Status{State: svc.StopPending}
	return false, 0
}

// Run starts the NATS server as a Windows service.
func Run(server *Server) error {
	if dockerized {
		server.Start()
		return nil
	}
	isWindowsService, err := svc.IsWindowsService()
	if err != nil {
		return err
	}
	if !isWindowsService {
		server.Start()
		return nil
	}
	return svc.Run(serviceName, &winServiceWrapper{server})
}

// isWindowsService indicates if NATS is running as a Windows service.
func isWindowsService() bool {
	if dockerized {
		return false
	}
	isWindowsService, _ := svc.IsWindowsService()
	return isWindowsService
}
