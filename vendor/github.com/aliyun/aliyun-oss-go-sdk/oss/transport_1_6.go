// +build !go1.7

package oss

import (
	"net"
	"net/http"
)

func newTransport(conn *Conn, config *Config) *http.Transport {
	httpTimeOut := conn.config.HTTPTimeout
	httpMaxConns := conn.config.HTTPMaxConns
	// New Transport
	transport := &http.Transport{
		Dial: func(netw, addr string) (net.Conn, error) {
			d := net.Dialer{Timeout: httpTimeOut.ConnectTimeout}
			if config.LocalAddr != nil {
				d.LocalAddr = config.LocalAddr
			}
			conn, err := d.Dial(netw, addr)
			if err != nil {
				return nil, err
			}
			return newTimeoutConn(conn, httpTimeOut.ReadWriteTimeout, httpTimeOut.LongTimeout), nil
		},
		MaxIdleConnsPerHost:   httpMaxConns.MaxIdleConnsPerHost,
		ResponseHeaderTimeout: httpTimeOut.HeaderTimeout,
	}
	return transport
}
