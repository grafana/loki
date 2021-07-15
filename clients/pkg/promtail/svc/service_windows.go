//+build windows

package svc

import (
	"time"

	wsvc "golang.org/x/sys/windows/svc"
)

type svcWindows struct{}

func init() {
	interactive, err := wsvc.IsAnInteractiveSession()
	if err != nil {
		panic(err)
	}
	if interactive {
		return
	}
	go func() {
		_ = wsvc.Run("", svcWindows{})
	}()
}

func (svcWindows) Execute(args []string, r <-chan wsvc.ChangeRequest, s chan<- wsvc.Status) (ssec bool, errno uint32) {
	const accCommands = wsvc.AcceptStop | wsvc.AcceptShutdown
	s <- wsvc.Status{State: wsvc.StartPending}
	s <- wsvc.Status{State: wsvc.Running, Accepts: accCommands}
	for {
		c := <-r
		switch c.Cmd {
		case wsvc.Interrogate:
			s <- c.CurrentStatus
			// Testing deadlock from https://code.google.com/p/winsvc/issues/detail?id=4
			time.Sleep(100 * time.Millisecond)
			s <- c.CurrentStatus
		case wsvc.Stop, wsvc.Shutdown:
			s <- wsvc.Status{State: wsvc.StopPending}
			return false, 0
		}
	}
	return false, 0
}
