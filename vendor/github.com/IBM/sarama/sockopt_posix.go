//go:build unix && !android && !illumos && !ios && !hurd

// build on aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package sarama

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

func getTCPConnSockError(conn *net.TCPConn) error {
	rawConn, err := conn.SyscallConn()
	if err != nil {
		return fmt.Errorf("failed to get raw connection: %w", err)
	}

	var sockErr int
	var opErr error

	err = rawConn.Control(func(fd uintptr) {
		sockErr, opErr = unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_ERROR)
	})
	if err != nil {
		return fmt.Errorf("failed to control raw connection: %w", err)
	}
	if opErr != nil {
		return fmt.Errorf("failed to get socket error: %w", opErr)
	}
	if sockErr != 0 {
		return unix.Errno(sockErr)
	}
	return nil
}
