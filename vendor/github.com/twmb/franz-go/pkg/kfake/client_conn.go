package kfake

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"time"

	"github.com/twmb/franz-go/pkg/kbin"
	"github.com/twmb/franz-go/pkg/kmsg"
)

type (
	clientConn struct {
		c      *Cluster
		b      *broker
		conn   net.Conn
		br     *bufio.Reader // buffers reads so a small request's size and body usually arrive in one read
		respCh chan clientResp
		done   chan struct{} // closed when read() returns
		mute   chan bool     // capacity 1: serializes request processing per connection; false = stop reading

		saslStage saslStage
		s0        *scramServer0
		user      string // authenticated user, set after SASL completes

		// hasSessionExpiry is set at authenticate time when
		// connections.max.reauth.ms is positive. Re-authentication is
		// gated on the connection's stored session expiration, not the
		// live config (KafkaChannel.maybeBeginServerReauthentication
		// checks the authenticator's session expiration time), so a
		// config change affects only sessions established after it.
		hasSessionExpiry bool
	}

	clientReq struct {
		cc     *clientConn
		kreq   kmsg.Request
		at     time.Time
		cid    string
		corr   int32
		faults *faultCheck // faults that can match this request, see Fault

		// Pre-validated error topics to merge into the response,
		// used when TopicID resolution fails for some topics while
		// the rest proceed through normal handling.
		offsetCommitErrTopics []kmsg.OffsetCommitResponseTopic
	}

	clientResp struct {
		kresp kmsg.Response
		corr  int32
		err   error
		skip  bool // acks=0 produce: nothing to write, just unmute
	}
)

func (creq *clientReq) empty() bool { return creq == nil || creq.cc == nil || creq.kreq == nil }

// unmute signals the read goroutine that it may submit the next request.
// ok=true means the prior response was written successfully; ok=false
// tells read to stop (the connection is dead).
func (cc *clientConn) unmute(ok bool) {
	select {
	case cc.mute <- ok:
	case <-cc.done:
	case <-cc.c.die:
	}
}

// reply sends a response back to the client, respecting connection close
// and cluster shutdown. It returns false if the client will never see the
// response, either because the connection died or because the cluster is
// shutting down.
func (creq *clientReq) reply(kresp kmsg.Response) bool {
	select {
	case creq.cc.respCh <- clientResp{kresp: kresp, corr: creq.corr}:
		return true
	case <-creq.cc.done:
	case <-creq.cc.c.die:
	}
	return false
}

func (cc *clientConn) read() {
	defer close(cc.done)
	defer cc.conn.Close()

	// The cluster dying closes the connection, which is what returns us
	// from a blocked read (and write from a blocked write).
	go func() {
		select {
		case <-cc.c.die:
			cc.conn.Close()
		case <-cc.done:
		}
	}()

	var (
		who  = cc.conn.RemoteAddr()
		size = make([]byte, 4)
	)
	for {
		if _, err := io.ReadFull(cc.br, size); err != nil {
			return
		}
		body := make([]byte, binary.BigEndian.Uint32(size))
		if _, err := io.ReadFull(cc.br, body); err != nil {
			return
		}

		var (
			reader   = kbin.Reader{Src: body}
			key      = reader.Int16()
			version  = reader.Int16()
			corr     = reader.Int32()
			clientID = reader.NullableString()
			kreq     = kmsg.RequestForKey(key)
		)
		kreq.SetVersion(version)
		if kreq.IsFlexible() {
			kmsg.SkipTags(&reader)
		}
		if err := kreq.ReadFrom(reader.Src); err != nil {
			cc.c.cfg.logger.Logf(LogLevelDebug, "client %s unable to parse request (key=%d, version=%d): %v", who, key, version, err)
			return
		}

		// Within Kafka, a null client ID is treated as an empty string.
		var cid string
		if clientID != nil {
			cid = *clientID
		}

		// Wait until the previous request's response has been fully
		// written before submitting the next. This matches the real
		// Kafka broker's per-connection serial request processing.
		// write() sends true after a successful write, false on error.
		// The channel is pre-filled with true so the first request
		// proceeds immediately.
		select {
		case ok := <-cc.mute:
			if !ok {
				return
			}
		case <-cc.c.die:
			return
		}
		select {
		case cc.c.reqCh <- &clientReq{cc: cc, kreq: kreq, at: time.Now(), cid: cid, corr: corr}:
		case <-cc.c.die:
			return
		}
	}
}

func (cc *clientConn) write() {
	defer cc.conn.Close()

	var (
		who = cc.conn.RemoteAddr()
		buf []byte
	)
	for {
		var resp clientResp
		select {
		case resp = <-cc.respCh:
		case <-cc.done:
			return
		case <-cc.c.die:
			return
		}
		// acks=0 produce: no response bytes exist, we only unmute so
		// that read() can submit the next request.
		if resp.skip {
			cc.unmute(true)
			continue
		}
		if err := resp.err; err != nil {
			cc.c.cfg.logger.Logf(LogLevelInfo, "client %s request unable to be handled: %v", who, err)
			cc.unmute(false)
			return
		}

		buf = append(buf[:0], 0, 0, 0, 0, 0, 0, 0, 0) // size (4) + correlation ID (4)
		if resp.kresp.IsFlexible() && resp.kresp.Key() != 18 {
			buf = append(buf, 0) // empty tagged fields section
		}
		buf = resp.kresp.AppendTo(buf)

		binary.BigEndian.PutUint32(buf[:4], uint32(len(buf)-4))
		binary.BigEndian.PutUint32(buf[4:8], uint32(resp.corr))

		_, err := cc.conn.Write(buf)
		cc.unmute(err == nil)
		if err != nil {
			return
		}
	}
}
