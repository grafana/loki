//go:build !unix || android || illumos || ios || hurd

package sarama

import "net"

func getTCPConnSockError(_ *net.TCPConn) error {
	return nil
}
