// Copyright 2012-2020 The NATS Authors
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
	"bufio"
	"bytes"
	"fmt"
	"net/http"
	"net/textproto"
)

type parserState int
type parseState struct {
	state   parserState
	op      byte
	as      int
	drop    int
	pa      pubArg
	argBuf  []byte
	msgBuf  []byte
	header  http.Header // access via getHeader
	scratch [MAX_CONTROL_LINE_SIZE]byte
}

type pubArg struct {
	arg     []byte
	pacache []byte
	origin  []byte
	account []byte
	subject []byte
	deliver []byte
	mapped  []byte
	reply   []byte
	szb     []byte
	hdb     []byte
	queues  [][]byte
	size    int
	hdr     int
	psi     []*serviceImport
}

// Parser constants
const (
	OP_START parserState = iota
	OP_PLUS
	OP_PLUS_O
	OP_PLUS_OK
	OP_MINUS
	OP_MINUS_E
	OP_MINUS_ER
	OP_MINUS_ERR
	OP_MINUS_ERR_SPC
	MINUS_ERR_ARG
	OP_C
	OP_CO
	OP_CON
	OP_CONN
	OP_CONNE
	OP_CONNEC
	OP_CONNECT
	CONNECT_ARG
	OP_H
	OP_HP
	OP_HPU
	OP_HPUB
	OP_HPUB_SPC
	HPUB_ARG
	OP_HM
	OP_HMS
	OP_HMSG
	OP_HMSG_SPC
	HMSG_ARG
	OP_P
	OP_PU
	OP_PUB
	OP_PUB_SPC
	PUB_ARG
	OP_PI
	OP_PIN
	OP_PING
	OP_PO
	OP_PON
	OP_PONG
	MSG_PAYLOAD
	MSG_END_R
	MSG_END_N
	OP_S
	OP_SU
	OP_SUB
	OP_SUB_SPC
	SUB_ARG
	OP_A
	OP_ASUB
	OP_ASUB_SPC
	ASUB_ARG
	OP_AUSUB
	OP_AUSUB_SPC
	AUSUB_ARG
	OP_L
	OP_LS
	OP_R
	OP_RS
	OP_U
	OP_UN
	OP_UNS
	OP_UNSU
	OP_UNSUB
	OP_UNSUB_SPC
	UNSUB_ARG
	OP_M
	OP_MS
	OP_MSG
	OP_MSG_SPC
	MSG_ARG
	OP_I
	OP_IN
	OP_INF
	OP_INFO
	INFO_ARG
)

func (c *client) parse(buf []byte) error {
	// Branch out to mqtt clients. c.mqtt is immutable, but should it become
	// an issue (say data race detection), we could branch outside in readLoop
	if c.isMqtt() {
		return c.mqttParse(buf)
	}
	var i int
	var b byte
	var lmsg bool

	// Snapshots
	c.mu.Lock()
	// Snapshot and then reset when we receive a
	// proper CONNECT if needed.
	authSet := c.awaitingAuth()
	// Snapshot max control line as well.
	s, mcl, trace := c.srv, c.mcl, c.trace
	c.mu.Unlock()

	// Move to loop instead of range syntax to allow jumping of i
	for i = 0; i < len(buf); i++ {
		b = buf[i]

		switch c.state {
		case OP_START:
			c.op = b
			if b != 'C' && b != 'c' {
				if authSet {
					if s == nil {
						goto authErr
					}
					var ok bool
					// Check here for NoAuthUser. If this is set allow non CONNECT protos as our first.
					// E.g. telnet proto demos.
					if noAuthUser := s.getOpts().NoAuthUser; noAuthUser != _EMPTY_ {
						s.mu.Lock()
						user, exists := s.users[noAuthUser]
						s.mu.Unlock()
						if exists {
							c.RegisterUser(user)
							c.mu.Lock()
							c.clearAuthTimer()
							c.flags.set(connectReceived)
							c.mu.Unlock()
							authSet, ok = false, true
						}
					}
					if !ok {
						goto authErr
					}
				}
				// If the connection is a gateway connection, make sure that
				// if this is an inbound, it starts with a CONNECT.
				if c.kind == GATEWAY && !c.gw.outbound && !c.gw.connected {
					// Use auth violation since no CONNECT was sent.
					// It could be a parseErr too.
					goto authErr
				}
			}
			switch b {
			case 'P', 'p':
				c.state = OP_P
			case 'H', 'h':
				c.state = OP_H
			case 'S', 's':
				c.state = OP_S
			case 'U', 'u':
				c.state = OP_U
			case 'R', 'r':
				if c.kind == CLIENT {
					goto parseErr
				} else {
					c.state = OP_R
				}
			case 'L', 'l':
				if c.kind != LEAF && c.kind != ROUTER {
					goto parseErr
				} else {
					c.state = OP_L
				}
			case 'A', 'a':
				if c.kind == CLIENT {
					goto parseErr
				} else {
					c.state = OP_A
				}
			case 'C', 'c':
				c.state = OP_C
			case 'I', 'i':
				c.state = OP_I
			case '+':
				c.state = OP_PLUS
			case '-':
				c.state = OP_MINUS
			default:
				goto parseErr
			}
		case OP_H:
			switch b {
			case 'P', 'p':
				c.state = OP_HP
			case 'M', 'm':
				c.state = OP_HM
			default:
				goto parseErr
			}
		case OP_HP:
			switch b {
			case 'U', 'u':
				c.state = OP_HPU
			default:
				goto parseErr
			}
		case OP_HPU:
			switch b {
			case 'B', 'b':
				c.state = OP_HPUB
			default:
				goto parseErr
			}
		case OP_HPUB:
			switch b {
			case ' ', '\t':
				c.state = OP_HPUB_SPC
			default:
				goto parseErr
			}
		case OP_HPUB_SPC:
			switch b {
			case ' ', '\t':
				continue
			default:
				c.pa.hdr = 0
				c.state = HPUB_ARG
				c.as = i
			}
		case HPUB_ARG:
			switch b {
			case '\r':
				c.drop = 1
			case '\n':
				var arg []byte
				if c.argBuf != nil {
					arg = c.argBuf
					c.argBuf = nil
				} else {
					arg = buf[c.as : i-c.drop]
				}
				if err := c.overMaxControlLineLimit(arg, mcl); err != nil {
					return err
				}
				if trace {
					c.traceInOp("HPUB", arg)
				}
				if err := c.processHeaderPub(arg); err != nil {
					return err
				}

				c.drop, c.as, c.state = 0, i+1, MSG_PAYLOAD
				// If we don't have a saved buffer then jump ahead with
				// the index. If this overruns what is left we fall out
				// and process split buffer.
				if c.msgBuf == nil {
					i = c.as + c.pa.size - LEN_CR_LF
				}
			default:
				if c.argBuf != nil {
					c.argBuf = append(c.argBuf, b)
				}
			}
		case OP_HM:
			switch b {
			case 'S', 's':
				c.state = OP_HMS
			default:
				goto parseErr
			}
		case OP_HMS:
			switch b {
			case 'G', 'g':
				c.state = OP_HMSG
			default:
				goto parseErr
			}
		case OP_HMSG:
			switch b {
			case ' ', '\t':
				c.state = OP_HMSG_SPC
			default:
				goto parseErr
			}
		case OP_HMSG_SPC:
			switch b {
			case ' ', '\t':
				continue
			default:
				c.pa.hdr = 0
				c.state = HMSG_ARG
				c.as = i
			}
		case HMSG_ARG:
			switch b {
			case '\r':
				c.drop = 1
			case '\n':
				var arg []byte
				if c.argBuf != nil {
					arg = c.argBuf
					c.argBuf = nil
				} else {
					arg = buf[c.as : i-c.drop]
				}
				if err := c.overMaxControlLineLimit(arg, mcl); err != nil {
					return err
				}
				var err error
				if c.kind == ROUTER || c.kind == GATEWAY {
					if trace {
						c.traceInOp("HMSG", arg)
					}
					err = c.processRoutedHeaderMsgArgs(arg)
				} else if c.kind == LEAF {
					if trace {
						c.traceInOp("HMSG", arg)
					}
					err = c.processLeafHeaderMsgArgs(arg)
				}
				if err != nil {
					return err
				}
				c.drop, c.as, c.state = 0, i+1, MSG_PAYLOAD

				// jump ahead with the index. If this overruns
				// what is left we fall out and process split
				// buffer.
				i = c.as + c.pa.size - LEN_CR_LF
			default:
				if c.argBuf != nil {
					c.argBuf = append(c.argBuf, b)
				}
			}
		case OP_P:
			switch b {
			case 'U', 'u':
				c.state = OP_PU
			case 'I', 'i':
				c.state = OP_PI
			case 'O', 'o':
				c.state = OP_PO
			default:
				goto parseErr
			}
		case OP_PU:
			switch b {
			case 'B', 'b':
				c.state = OP_PUB
			default:
				goto parseErr
			}
		case OP_PUB:
			switch b {
			case ' ', '\t':
				c.state = OP_PUB_SPC
			default:
				goto parseErr
			}
		case OP_PUB_SPC:
			switch b {
			case ' ', '\t':
				continue
			default:
				c.pa.hdr = -1
				c.state = PUB_ARG
				c.as = i
			}
		case PUB_ARG:
			switch b {
			case '\r':
				c.drop = 1
			case '\n':
				var arg []byte
				if c.argBuf != nil {
					arg = c.argBuf
					c.argBuf = nil
				} else {
					arg = buf[c.as : i-c.drop]
				}
				if err := c.overMaxControlLineLimit(arg, mcl); err != nil {
					return err
				}
				if trace {
					c.traceInOp("PUB", arg)
				}
				if err := c.processPub(arg); err != nil {
					return err
				}

				c.drop, c.as, c.state = 0, i+1, MSG_PAYLOAD
				// If we don't have a saved buffer then jump ahead with
				// the index. If this overruns what is left we fall out
				// and process split buffer.
				if c.msgBuf == nil {
					i = c.as + c.pa.size - LEN_CR_LF
				}
			default:
				if c.argBuf != nil {
					c.argBuf = append(c.argBuf, b)
				}
			}
		case MSG_PAYLOAD:
			if c.msgBuf != nil {
				// copy as much as we can to the buffer and skip ahead.
				toCopy := c.pa.size - len(c.msgBuf)
				avail := len(buf) - i
				if avail < toCopy {
					toCopy = avail
				}
				if toCopy > 0 {
					start := len(c.msgBuf)
					// This is needed for copy to work.
					c.msgBuf = c.msgBuf[:start+toCopy]
					copy(c.msgBuf[start:], buf[i:i+toCopy])
					// Update our index
					i = (i + toCopy) - 1
				} else {
					// Fall back to append if needed.
					c.msgBuf = append(c.msgBuf, b)
				}
				if len(c.msgBuf) >= c.pa.size {
					c.state = MSG_END_R
				}
			} else if i-c.as+1 >= c.pa.size {
				c.state = MSG_END_R
			}
		case MSG_END_R:
			if b != '\r' {
				goto parseErr
			}
			if c.msgBuf != nil {
				c.msgBuf = append(c.msgBuf, b)
			}
			c.state = MSG_END_N
		case MSG_END_N:
			if b != '\n' {
				goto parseErr
			}
			if c.msgBuf != nil {
				c.msgBuf = append(c.msgBuf, b)
			} else {
				c.msgBuf = buf[c.as : i+1]
			}

			// Check for mappings.
			if (c.kind == CLIENT || c.kind == LEAF) && c.in.flags.isSet(hasMappings) {
				changed := c.selectMappedSubject()
				if trace && changed {
					c.traceInOp("MAPPING", []byte(fmt.Sprintf("%s -> %s", c.pa.mapped, c.pa.subject)))
				}
			}
			if trace {
				c.traceMsg(c.msgBuf)
			}

			c.processInboundMsg(c.msgBuf)
			c.argBuf, c.msgBuf, c.header = nil, nil, nil
			c.drop, c.as, c.state = 0, i+1, OP_START
			// Drop all pub args
			c.pa.arg, c.pa.pacache, c.pa.origin, c.pa.account, c.pa.subject, c.pa.mapped = nil, nil, nil, nil, nil, nil
			c.pa.reply, c.pa.hdr, c.pa.size, c.pa.szb, c.pa.hdb, c.pa.queues = nil, -1, 0, nil, nil, nil
			lmsg = false
		case OP_A:
			switch b {
			case '+':
				c.state = OP_ASUB
			case '-', 'u':
				c.state = OP_AUSUB
			default:
				goto parseErr
			}
		case OP_ASUB:
			switch b {
			case ' ', '\t':
				c.state = OP_ASUB_SPC
			default:
				goto parseErr
			}
		case OP_ASUB_SPC:
			switch b {
			case ' ', '\t':
				continue
			default:
				c.state = ASUB_ARG
				c.as = i
			}
		case ASUB_ARG:
			switch b {
			case '\r':
				c.drop = 1
			case '\n':
				var arg []byte
				if c.argBuf != nil {
					arg = c.argBuf
					c.argBuf = nil
				} else {
					arg = buf[c.as : i-c.drop]
				}
				if err := c.overMaxControlLineLimit(arg, mcl); err != nil {
					return err
				}
				if trace {
					c.traceInOp("A+", arg)
				}
				if err := c.processAccountSub(arg); err != nil {
					return err
				}
				c.drop, c.as, c.state = 0, i+1, OP_START
			default:
				if c.argBuf != nil {
					c.argBuf = append(c.argBuf, b)
				}
			}
		case OP_AUSUB:
			switch b {
			case ' ', '\t':
				c.state = OP_AUSUB_SPC
			default:
				goto parseErr
			}
		case OP_AUSUB_SPC:
			switch b {
			case ' ', '\t':
				continue
			default:
				c.state = AUSUB_ARG
				c.as = i
			}
		case AUSUB_ARG:
			switch b {
			case '\r':
				c.drop = 1
			case '\n':
				var arg []byte
				if c.argBuf != nil {
					arg = c.argBuf
					c.argBuf = nil
				} else {
					arg = buf[c.as : i-c.drop]
				}
				if err := c.overMaxControlLineLimit(arg, mcl); err != nil {
					return err
				}
				if trace {
					c.traceInOp("A-", arg)
				}
				c.processAccountUnsub(arg)
				c.drop, c.as, c.state = 0, i+1, OP_START
			default:
				if c.argBuf != nil {
					c.argBuf = append(c.argBuf, b)
				}
			}
		case OP_S:
			switch b {
			case 'U', 'u':
				c.state = OP_SU
			default:
				goto parseErr
			}
		case OP_SU:
			switch b {
			case 'B', 'b':
				c.state = OP_SUB
			default:
				goto parseErr
			}
		case OP_SUB:
			switch b {
			case ' ', '\t':
				c.state = OP_SUB_SPC
			default:
				goto parseErr
			}
		case OP_SUB_SPC:
			switch b {
			case ' ', '\t':
				continue
			default:
				c.state = SUB_ARG
				c.as = i
			}
		case SUB_ARG:
			switch b {
			case '\r':
				c.drop = 1
			case '\n':
				var arg []byte
				if c.argBuf != nil {
					arg = c.argBuf
					c.argBuf = nil
				} else {
					arg = buf[c.as : i-c.drop]
				}
				if err := c.overMaxControlLineLimit(arg, mcl); err != nil {
					return err
				}
				var err error

				switch c.kind {
				case CLIENT:
					if trace {
						c.traceInOp("SUB", arg)
					}
					err = c.parseSub(arg, false)
				case ROUTER:
					switch c.op {
					case 'R', 'r':
						if trace {
							c.traceInOp("RS+", arg)
						}
						err = c.processRemoteSub(arg, false)
					case 'L', 'l':
						if trace {
							c.traceInOp("LS+", arg)
						}
						err = c.processRemoteSub(arg, true)
					}
				case GATEWAY:
					if trace {
						c.traceInOp("RS+", arg)
					}
					err = c.processGatewayRSub(arg)
				case LEAF:
					if trace {
						c.traceInOp("LS+", arg)
					}
					err = c.processLeafSub(arg)
				}
				if err != nil {
					return err
				}
				c.drop, c.as, c.state = 0, i+1, OP_START
			default:
				if c.argBuf != nil {
					c.argBuf = append(c.argBuf, b)
				}
			}
		case OP_L:
			switch b {
			case 'S', 's':
				c.state = OP_LS
			case 'M', 'm':
				c.state = OP_M
			default:
				goto parseErr
			}
		case OP_LS:
			switch b {
			case '+':
				c.state = OP_SUB
			case '-':
				c.state = OP_UNSUB
			default:
				goto parseErr
			}
		case OP_R:
			switch b {
			case 'S', 's':
				c.state = OP_RS
			case 'M', 'm':
				c.state = OP_M
			default:
				goto parseErr
			}
		case OP_RS:
			switch b {
			case '+':
				c.state = OP_SUB
			case '-':
				c.state = OP_UNSUB
			default:
				goto parseErr
			}
		case OP_U:
			switch b {
			case 'N', 'n':
				c.state = OP_UN
			default:
				goto parseErr
			}
		case OP_UN:
			switch b {
			case 'S', 's':
				c.state = OP_UNS
			default:
				goto parseErr
			}
		case OP_UNS:
			switch b {
			case 'U', 'u':
				c.state = OP_UNSU
			default:
				goto parseErr
			}
		case OP_UNSU:
			switch b {
			case 'B', 'b':
				c.state = OP_UNSUB
			default:
				goto parseErr
			}
		case OP_UNSUB:
			switch b {
			case ' ', '\t':
				c.state = OP_UNSUB_SPC
			default:
				goto parseErr
			}
		case OP_UNSUB_SPC:
			switch b {
			case ' ', '\t':
				continue
			default:
				c.state = UNSUB_ARG
				c.as = i
			}
		case UNSUB_ARG:
			switch b {
			case '\r':
				c.drop = 1
			case '\n':
				var arg []byte
				if c.argBuf != nil {
					arg = c.argBuf
					c.argBuf = nil
				} else {
					arg = buf[c.as : i-c.drop]
				}
				if err := c.overMaxControlLineLimit(arg, mcl); err != nil {
					return err
				}
				var err error

				switch c.kind {
				case CLIENT:
					if trace {
						c.traceInOp("UNSUB", arg)
					}
					err = c.processUnsub(arg)
				case ROUTER:
					if trace && c.srv != nil {
						switch c.op {
						case 'R', 'r':
							c.traceInOp("RS-", arg)
						case 'L', 'l':
							c.traceInOp("LS-", arg)
						}
					}
					err = c.processRemoteUnsub(arg)
				case GATEWAY:
					if trace {
						c.traceInOp("RS-", arg)
					}
					err = c.processGatewayRUnsub(arg)
				case LEAF:
					if trace {
						c.traceInOp("LS-", arg)
					}
					err = c.processLeafUnsub(arg)
				}
				if err != nil {
					return err
				}
				c.drop, c.as, c.state = 0, i+1, OP_START
			default:
				if c.argBuf != nil {
					c.argBuf = append(c.argBuf, b)
				}
			}
		case OP_PI:
			switch b {
			case 'N', 'n':
				c.state = OP_PIN
			default:
				goto parseErr
			}
		case OP_PIN:
			switch b {
			case 'G', 'g':
				c.state = OP_PING
			default:
				goto parseErr
			}
		case OP_PING:
			switch b {
			case '\n':
				if trace {
					c.traceInOp("PING", nil)
				}
				c.processPing()
				c.drop, c.state = 0, OP_START
			}
		case OP_PO:
			switch b {
			case 'N', 'n':
				c.state = OP_PON
			default:
				goto parseErr
			}
		case OP_PON:
			switch b {
			case 'G', 'g':
				c.state = OP_PONG
			default:
				goto parseErr
			}
		case OP_PONG:
			switch b {
			case '\n':
				if trace {
					c.traceInOp("PONG", nil)
				}
				c.processPong()
				c.drop, c.state = 0, OP_START
			}
		case OP_C:
			switch b {
			case 'O', 'o':
				c.state = OP_CO
			default:
				goto parseErr
			}
		case OP_CO:
			switch b {
			case 'N', 'n':
				c.state = OP_CON
			default:
				goto parseErr
			}
		case OP_CON:
			switch b {
			case 'N', 'n':
				c.state = OP_CONN
			default:
				goto parseErr
			}
		case OP_CONN:
			switch b {
			case 'E', 'e':
				c.state = OP_CONNE
			default:
				goto parseErr
			}
		case OP_CONNE:
			switch b {
			case 'C', 'c':
				c.state = OP_CONNEC
			default:
				goto parseErr
			}
		case OP_CONNEC:
			switch b {
			case 'T', 't':
				c.state = OP_CONNECT
			default:
				goto parseErr
			}
		case OP_CONNECT:
			switch b {
			case ' ', '\t':
				continue
			default:
				c.state = CONNECT_ARG
				c.as = i
			}
		case CONNECT_ARG:
			switch b {
			case '\r':
				c.drop = 1
			case '\n':
				var arg []byte
				if c.argBuf != nil {
					arg = c.argBuf
					c.argBuf = nil
				} else {
					arg = buf[c.as : i-c.drop]
				}
				if err := c.overMaxControlLineLimit(arg, mcl); err != nil {
					return err
				}
				if trace {
					c.traceInOp("CONNECT", removePassFromTrace(arg))
				}
				if err := c.processConnect(arg); err != nil {
					return err
				}
				c.drop, c.state = 0, OP_START
				// Reset notion on authSet
				c.mu.Lock()
				authSet = c.awaitingAuth()
				c.mu.Unlock()
			default:
				if c.argBuf != nil {
					c.argBuf = append(c.argBuf, b)
				}
			}
		case OP_M:
			switch b {
			case 'S', 's':
				c.state = OP_MS
			default:
				goto parseErr
			}
		case OP_MS:
			switch b {
			case 'G', 'g':
				c.state = OP_MSG
			default:
				goto parseErr
			}
		case OP_MSG:
			switch b {
			case ' ', '\t':
				c.state = OP_MSG_SPC
			default:
				goto parseErr
			}
		case OP_MSG_SPC:
			switch b {
			case ' ', '\t':
				continue
			default:
				c.pa.hdr = -1
				c.state = MSG_ARG
				c.as = i
			}
		case MSG_ARG:
			switch b {
			case '\r':
				c.drop = 1
			case '\n':
				var arg []byte
				if c.argBuf != nil {
					arg = c.argBuf
					c.argBuf = nil
				} else {
					arg = buf[c.as : i-c.drop]
				}
				if err := c.overMaxControlLineLimit(arg, mcl); err != nil {
					return err
				}
				var err error
				if c.kind == ROUTER || c.kind == GATEWAY {
					switch c.op {
					case 'R', 'r':
						if trace {
							c.traceInOp("RMSG", arg)
						}
						err = c.processRoutedMsgArgs(arg)
					case 'L', 'l':
						if trace {
							c.traceInOp("LMSG", arg)
						}
						lmsg = true
						err = c.processRoutedOriginClusterMsgArgs(arg)
					}
				} else if c.kind == LEAF {
					if trace {
						c.traceInOp("LMSG", arg)
					}
					err = c.processLeafMsgArgs(arg)
				}
				if err != nil {
					return err
				}
				c.drop, c.as, c.state = 0, i+1, MSG_PAYLOAD

				// jump ahead with the index. If this overruns
				// what is left we fall out and process split
				// buffer.
				i = c.as + c.pa.size - LEN_CR_LF
			default:
				if c.argBuf != nil {
					c.argBuf = append(c.argBuf, b)
				}
			}
		case OP_I:
			switch b {
			case 'N', 'n':
				c.state = OP_IN
			default:
				goto parseErr
			}
		case OP_IN:
			switch b {
			case 'F', 'f':
				c.state = OP_INF
			default:
				goto parseErr
			}
		case OP_INF:
			switch b {
			case 'O', 'o':
				c.state = OP_INFO
			default:
				goto parseErr
			}
		case OP_INFO:
			switch b {
			case ' ', '\t':
				continue
			default:
				c.state = INFO_ARG
				c.as = i
			}
		case INFO_ARG:
			switch b {
			case '\r':
				c.drop = 1
			case '\n':
				var arg []byte
				if c.argBuf != nil {
					arg = c.argBuf
					c.argBuf = nil
				} else {
					arg = buf[c.as : i-c.drop]
				}
				if err := c.overMaxControlLineLimit(arg, mcl); err != nil {
					return err
				}
				if err := c.processInfo(arg); err != nil {
					return err
				}
				c.drop, c.as, c.state = 0, i+1, OP_START
			default:
				if c.argBuf != nil {
					c.argBuf = append(c.argBuf, b)
				}
			}
		case OP_PLUS:
			switch b {
			case 'O', 'o':
				c.state = OP_PLUS_O
			default:
				goto parseErr
			}
		case OP_PLUS_O:
			switch b {
			case 'K', 'k':
				c.state = OP_PLUS_OK
			default:
				goto parseErr
			}
		case OP_PLUS_OK:
			switch b {
			case '\n':
				c.drop, c.state = 0, OP_START
			}
		case OP_MINUS:
			switch b {
			case 'E', 'e':
				c.state = OP_MINUS_E
			default:
				goto parseErr
			}
		case OP_MINUS_E:
			switch b {
			case 'R', 'r':
				c.state = OP_MINUS_ER
			default:
				goto parseErr
			}
		case OP_MINUS_ER:
			switch b {
			case 'R', 'r':
				c.state = OP_MINUS_ERR
			default:
				goto parseErr
			}
		case OP_MINUS_ERR:
			switch b {
			case ' ', '\t':
				c.state = OP_MINUS_ERR_SPC
			default:
				goto parseErr
			}
		case OP_MINUS_ERR_SPC:
			switch b {
			case ' ', '\t':
				continue
			default:
				c.state = MINUS_ERR_ARG
				c.as = i
			}
		case MINUS_ERR_ARG:
			switch b {
			case '\r':
				c.drop = 1
			case '\n':
				var arg []byte
				if c.argBuf != nil {
					arg = c.argBuf
					c.argBuf = nil
				} else {
					arg = buf[c.as : i-c.drop]
				}
				if err := c.overMaxControlLineLimit(arg, mcl); err != nil {
					return err
				}
				c.processErr(string(arg))
				c.drop, c.as, c.state = 0, i+1, OP_START
			default:
				if c.argBuf != nil {
					c.argBuf = append(c.argBuf, b)
				}
			}
		default:
			goto parseErr
		}
	}

	// Check for split buffer scenarios for any ARG state.
	if c.state == SUB_ARG || c.state == UNSUB_ARG ||
		c.state == PUB_ARG || c.state == HPUB_ARG ||
		c.state == ASUB_ARG || c.state == AUSUB_ARG ||
		c.state == MSG_ARG || c.state == HMSG_ARG ||
		c.state == MINUS_ERR_ARG || c.state == CONNECT_ARG || c.state == INFO_ARG {

		// Setup a holder buffer to deal with split buffer scenario.
		if c.argBuf == nil {
			c.argBuf = c.scratch[:0]
			c.argBuf = append(c.argBuf, buf[c.as:i-c.drop]...)
		}
		// Check for violations of control line length here. Note that this is not
		// exact at all but the performance hit is too great to be precise, and
		// catching here should prevent memory exhaustion attacks.
		if err := c.overMaxControlLineLimit(c.argBuf, mcl); err != nil {
			return err
		}
	}

	// Check for split msg
	if (c.state == MSG_PAYLOAD || c.state == MSG_END_R || c.state == MSG_END_N) && c.msgBuf == nil {
		// We need to clone the pubArg if it is still referencing the
		// read buffer and we are not able to process the msg.

		if c.argBuf == nil {
			// Works also for MSG_ARG, when message comes from ROUTE or GATEWAY.
			if err := c.clonePubArg(lmsg); err != nil {
				goto parseErr
			}
		}

		// If we will overflow the scratch buffer, just create a
		// new buffer to hold the split message.
		if c.pa.size > cap(c.scratch)-len(c.argBuf) {
			lrem := len(buf[c.as:])
			// Consider it a protocol error when the remaining payload
			// is larger than the reported size for PUB. It can happen
			// when processing incomplete messages from rogue clients.
			if lrem > c.pa.size+LEN_CR_LF {
				goto parseErr
			}
			c.msgBuf = make([]byte, lrem, c.pa.size+LEN_CR_LF)
			copy(c.msgBuf, buf[c.as:])
		} else {
			c.msgBuf = c.scratch[len(c.argBuf):len(c.argBuf)]
			c.msgBuf = append(c.msgBuf, (buf[c.as:])...)
		}
	}

	return nil

authErr:
	c.authViolation()
	return ErrAuthentication

parseErr:
	c.sendErr("Unknown Protocol Operation")
	snip := protoSnippet(i, PROTO_SNIPPET_SIZE, buf)
	err := fmt.Errorf("%s parser ERROR, state=%d, i=%d: proto='%s...'", c.kindString(), c.state, i, snip)
	return err
}

func protoSnippet(start, max int, buf []byte) string {
	stop := start + max
	bufSize := len(buf)
	if start >= bufSize {
		return `""`
	}
	if stop > bufSize {
		stop = bufSize - 1
	}
	return fmt.Sprintf("%q", buf[start:stop])
}

// Check if the length of buffer `arg` is over the max control line limit `mcl`.
// If so, an error is sent to the client and the connection is closed.
// The error ErrMaxControlLine is returned.
func (c *client) overMaxControlLineLimit(arg []byte, mcl int32) error {
	if c.kind != CLIENT {
		return nil
	}
	if len(arg) > int(mcl) {
		err := NewErrorCtx(ErrMaxControlLine, "State %d, max_control_line %d, Buffer len %d (snip: %s...)",
			c.state, int(mcl), len(c.argBuf), protoSnippet(0, MAX_CONTROL_LINE_SNIPPET_SIZE, arg))
		c.sendErr(err.Error())
		c.closeConnection(MaxControlLineExceeded)
		return err
	}
	return nil
}

// clonePubArg is used when the split buffer scenario has the pubArg in the existing read buffer, but
// we need to hold onto it into the next read.
func (c *client) clonePubArg(lmsg bool) error {
	// Just copy and re-process original arg buffer.
	c.argBuf = c.scratch[:0]
	c.argBuf = append(c.argBuf, c.pa.arg...)

	switch c.kind {
	case ROUTER, GATEWAY:
		if lmsg {
			return c.processRoutedOriginClusterMsgArgs(c.argBuf)
		}
		if c.pa.hdr < 0 {
			return c.processRoutedMsgArgs(c.argBuf)
		} else {
			return c.processRoutedHeaderMsgArgs(c.argBuf)
		}
	case LEAF:
		if c.pa.hdr < 0 {
			return c.processLeafMsgArgs(c.argBuf)
		} else {
			return c.processLeafHeaderMsgArgs(c.argBuf)
		}
	default:
		if c.pa.hdr < 0 {
			return c.processPub(c.argBuf)
		} else {
			return c.processHeaderPub(c.argBuf)
		}
	}
}

func (ps *parseState) getHeader() http.Header {
	if ps.header == nil {
		if hdr := ps.pa.hdr; hdr > 0 {
			reader := bufio.NewReader(bytes.NewReader(ps.msgBuf[0:hdr]))
			tp := textproto.NewReader(reader)
			tp.ReadLine() // skip over first line, contains version
			if mimeHeader, err := tp.ReadMIMEHeader(); err == nil {
				ps.header = http.Header(mimeHeader)
			}
		}
	}
	return ps.header
}
