// Go MySQL Driver - A MySQL-Driver for Go's database/sql package
//
// Copyright 2018 The Go-MySQL-Driver Authors. All rights reserved.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at http://mozilla.org/MPL/2.0/.

package mysql

import (
	"context"
	"database/sql/driver"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

type connector struct {
	cfg               *Config // immutable private copy.
	encodedAttributes string  // Encoded connection attributes.
}

func encodeConnectionAttributes(cfg *Config) string {
	connAttrsBuf := make([]byte, 0)

	// default connection attributes
	connAttrsBuf = appendLengthEncodedString(connAttrsBuf, connAttrClientName)
	connAttrsBuf = appendLengthEncodedString(connAttrsBuf, connAttrClientNameValue)
	connAttrsBuf = appendLengthEncodedString(connAttrsBuf, connAttrOS)
	connAttrsBuf = appendLengthEncodedString(connAttrsBuf, connAttrOSValue)
	connAttrsBuf = appendLengthEncodedString(connAttrsBuf, connAttrPlatform)
	connAttrsBuf = appendLengthEncodedString(connAttrsBuf, connAttrPlatformValue)
	connAttrsBuf = appendLengthEncodedString(connAttrsBuf, connAttrPid)
	connAttrsBuf = appendLengthEncodedString(connAttrsBuf, strconv.Itoa(os.Getpid()))
	serverHost, _, _ := net.SplitHostPort(cfg.Addr)
	if serverHost != "" {
		connAttrsBuf = appendLengthEncodedString(connAttrsBuf, connAttrServerHost)
		connAttrsBuf = appendLengthEncodedString(connAttrsBuf, serverHost)
	}

	// user-defined connection attributes
	for _, connAttr := range strings.Split(cfg.ConnectionAttributes, ",") {
		k, v, found := strings.Cut(connAttr, ":")
		if !found {
			continue
		}
		connAttrsBuf = appendLengthEncodedString(connAttrsBuf, k)
		connAttrsBuf = appendLengthEncodedString(connAttrsBuf, v)
	}

	return string(connAttrsBuf)
}

func newConnector(cfg *Config) *connector {
	encodedAttributes := encodeConnectionAttributes(cfg)
	return &connector{
		cfg:               cfg,
		encodedAttributes: encodedAttributes,
	}
}

// Connect implements driver.Connector interface.
// Connect returns a connection to the database.
func (c *connector) Connect(ctx context.Context) (driver.Conn, error) {
	var err error

	// Invoke beforeConnect if present, with a copy of the configuration
	cfg := c.cfg
	if c.cfg.beforeConnect != nil {
		cfg = c.cfg.Clone()
		err = c.cfg.beforeConnect(ctx, cfg)
		if err != nil {
			return nil, err
		}
	}

	// New mysqlConn
	mc := &mysqlConn{
		maxAllowedPacket: maxPacketSize,
		maxWriteSize:     maxPacketSize - 1,
		closech:          make(chan struct{}),
		cfg:              cfg,
		connector:        c,
	}
	mc.parseTime = mc.cfg.ParseTime

	// Connect to Server
	dctx := ctx
	if mc.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		dctx, cancel = context.WithTimeout(ctx, c.cfg.Timeout)
		defer cancel()
	}

	if c.cfg.DialFunc != nil {
		mc.netConn, err = c.cfg.DialFunc(dctx, mc.cfg.Net, mc.cfg.Addr)
	} else {
		dialsLock.RLock()
		dial, ok := dials[mc.cfg.Net]
		dialsLock.RUnlock()
		if ok {
			mc.netConn, err = dial(dctx, mc.cfg.Addr)
		} else {
			nd := net.Dialer{}
			mc.netConn, err = nd.DialContext(dctx, mc.cfg.Net, mc.cfg.Addr)
		}
	}
	if err != nil {
		return nil, err
	}
	mc.rawConn = mc.netConn

	// Enable TCP Keepalives on TCP connections
	if tc, ok := mc.netConn.(*net.TCPConn); ok {
		if err := tc.SetKeepAlive(true); err != nil {
			c.cfg.Logger.Print(err)
		}
	}

	// Call startWatcher for context support (From Go 1.8)
	mc.startWatcher()
	if err := mc.watchCancel(ctx); err != nil {
		mc.cleanup()
		return nil, err
	}
	defer mc.finish()

	mc.buf = newBuffer()

	// Reading Handshake Initialization Packet
	authData, plugin, err := mc.readHandshakePacket()
	if err != nil {
		mc.cleanup()
		return nil, err
	}

	if plugin == "" {
		plugin = defaultAuthPlugin
	}

	// Send Client Authentication Packet
	authResp, err := mc.auth(authData, plugin)
	if err != nil {
		// try the default auth plugin, if using the requested plugin failed
		c.cfg.Logger.Print("could not use requested auth plugin '"+plugin+"': ", err.Error())
		plugin = defaultAuthPlugin
		authResp, err = mc.auth(authData, plugin)
		if err != nil {
			mc.cleanup()
			return nil, err
		}
	}
	if err = mc.writeHandshakeResponsePacket(authResp, plugin); err != nil {
		mc.cleanup()
		return nil, err
	}

	// Handle response to auth packet, switch methods if possible
	if err = mc.handleAuthResult(authData, plugin); err != nil {
		// Authentication failed and MySQL has already closed the connection
		// (https://dev.mysql.com/doc/internals/en/authentication-fails.html).
		// Do not send COM_QUIT, just cleanup and return the error.
		mc.cleanup()
		return nil, err
	}

	if mc.cfg.compress && mc.flags&clientCompress == clientCompress {
		mc.compress = true
		mc.compIO = newCompIO(mc)
	}
	if mc.cfg.MaxAllowedPacket > 0 {
		mc.maxAllowedPacket = mc.cfg.MaxAllowedPacket
	} else {
		// Get max allowed packet size
		maxap, err := mc.getSystemVar("max_allowed_packet")
		if err != nil {
			mc.Close()
			return nil, err
		}
		n, err := strconv.Atoi(string(maxap))
		if err != nil {
			mc.Close()
			return nil, fmt.Errorf("invalid max_allowed_packet value (%q): %w", maxap, err)
		}
		mc.maxAllowedPacket = n - 1
	}
	if mc.maxAllowedPacket < maxPacketSize {
		mc.maxWriteSize = mc.maxAllowedPacket
	}

	// Charset: character_set_connection, character_set_client, character_set_results
	if len(mc.cfg.charsets) > 0 {
		for _, cs := range mc.cfg.charsets {
			// ignore errors here - a charset may not exist
			if mc.cfg.Collation != "" {
				err = mc.exec("SET NAMES " + cs + " COLLATE " + mc.cfg.Collation)
			} else {
				err = mc.exec("SET NAMES " + cs)
			}
			if err == nil {
				break
			}
		}
		if err != nil {
			mc.Close()
			return nil, err
		}
	}

	// Handle DSN Params
	err = mc.handleParams()
	if err != nil {
		mc.Close()
		return nil, err
	}

	return mc, nil
}

// Driver implements driver.Connector interface.
// Driver returns &MySQLDriver{}.
func (c *connector) Driver() driver.Driver {
	return &MySQLDriver{}
}
