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
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// Signal Handling
func (s *Server) handleSignals() {
	if s.getOpts().NoSigs {
		return
	}
	c := make(chan os.Signal, 1)

	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		for sig := range c {
			s.Debugf("Trapped %q signal", sig)
			s.Shutdown()
			os.Exit(0)
		}
	}()
}

// ProcessSignal sends the given signal command to the running nats-server service.
// If service is empty, this signals the "nats-server" service. This returns an
// error is the given service is not running or the command is invalid.
func ProcessSignal(command Command, service string) error {
	if service == "" {
		service = serviceName
	}

	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()

	s, err := m.OpenService(service)
	if err != nil {
		return fmt.Errorf("could not access service: %v", err)
	}
	defer s.Close()

	var (
		cmd svc.Cmd
		to  svc.State
	)

	switch command {
	case CommandStop, CommandQuit:
		cmd = svc.Stop
		to = svc.Stopped
	case CommandReopen:
		cmd = reopenLogCmd
		to = svc.Running
	case CommandReload:
		cmd = svc.ParamChange
		to = svc.Running
	case commandLDMode:
		cmd = ldmCmd
		to = svc.Running
	default:
		return fmt.Errorf("unknown signal %q", command)
	}

	status, err := s.Control(cmd)
	if err != nil {
		return fmt.Errorf("could not send control=%d: %v", cmd, err)
	}

	timeout := time.Now().Add(10 * time.Second)
	for status.State != to {
		if timeout.Before(time.Now()) {
			return fmt.Errorf("timeout waiting for service to go to state=%d", to)
		}
		time.Sleep(300 * time.Millisecond)
		status, err = s.Query()
		if err != nil {
			return fmt.Errorf("could not retrieve service status: %v", err)
		}
	}

	return nil
}
