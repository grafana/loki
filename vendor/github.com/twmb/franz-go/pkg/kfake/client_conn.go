package kfake

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"sync"
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
		done   chan struct{} // closed when read() or write() returns, see closeDone
		mute   chan struct{} // capacity 1: serializes request processing per connection

		doneOnce sync.Once

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
		seq    uint32
		faults *faultCheck // faults that can match this request, see Fault

		unmuted  bool          // a control slept this request with SleepOutOfOrder, releasing the mute
		controls []*controlCtx // controls already given this request; tryControlKey skips them

		// Pre-validated error topics to merge into the response,
		// used when TopicID resolution fails for some topics while
		// the rest proceed through normal handling.
		offsetCommitErrTopics []kmsg.OffsetCommitResponseTopic
	}

	clientResp struct {
		kresp   kmsg.Response
		corr    int32
		err     error
		seq     uint32
		unmuted bool // the request released the mute already; write does not unmute
		skip    bool // acks=0 produce: nothing to write, just unmute
	}
)

func (creq *clientReq) empty() bool { return creq == nil || creq.cc == nil || creq.kreq == nil }

// unmute signals the read goroutine that it may submit the next request.
func (cc *clientConn) unmute() {
	select {
	case cc.mute <- struct{}{}:
	case <-cc.done:
	case <-cc.c.die:
	}
}

// closeDone closes done when read or write returns, whichever is first. Once
// write returns, nothing drains respCh, so read and run's sends to respCh must
// stop waiting on this connection. With SleepOutOfOrder, read can be about to
// submit a request when write returns.
func (cc *clientConn) closeDone() { cc.doneOnce.Do(func() { close(cc.done) }) }

// reply sends a response back to the client, respecting connection close
// and cluster shutdown. It returns false if the client will never see the
// response, either because the connection died or because the cluster is
// shutting down.
func (creq *clientReq) reply(kresp kmsg.Response) bool {
	select {
	case creq.cc.respCh <- clientResp{kresp: kresp, corr: creq.corr, seq: creq.seq, unmuted: creq.unmuted}:
		return true
	case <-creq.cc.done:
	case <-creq.cc.c.die:
	}
	return false
}

func (cc *clientConn) read() {
	defer cc.closeDone()
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
		seq  uint32
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
		// write() refills mute after a successful write and closes
		// done when it returns. The channel is pre-filled so the
		// first request proceeds immediately.
		select {
		case <-cc.mute:
		case <-cc.done:
			return
		case <-cc.c.die:
			return
		}
		select {
		case cc.c.reqCh <- &clientReq{cc: cc, kreq: kreq, at: time.Now(), cid: cid, corr: corr, seq: seq}:
			seq++
		case <-cc.done:
			return
		case <-cc.c.die:
			return
		}
	}
}

func (cc *clientConn) write() {
	defer cc.closeDone()
	defer cc.conn.Close()

	var (
		who = cc.conn.RemoteAddr()
		buf []byte
		seq uint32

		// With SleepOutOfOrder, a request can be answered while an
		// earlier request on this connection sleeps. We hold its
		// response until every earlier request is answered, and
		// release its mute when we hold it so that read can submit
		// the next request in the meantime.
		oooresp map[uint32]clientResp
	)
	for {
		resp, ok := oooresp[seq]
		if ok {
			delete(oooresp, seq)
		} else {
			select {
			case resp = <-cc.respCh:
			case <-cc.done:
				return
			case <-cc.c.die:
				return
			}
			if resp.seq != seq {
				if !resp.unmuted {
					resp.unmuted = true
					cc.unmute()
				}
				if oooresp == nil {
					oooresp = make(map[uint32]clientResp)
				}
				oooresp[resp.seq] = resp
				continue
			}
		}
		seq++
		// acks=0 produce: no response bytes exist, we only unmute so
		// that read() can submit the next request.
		if resp.skip {
			if !resp.unmuted {
				cc.unmute()
			}
			continue
		}
		if err := resp.err; err != nil {
			cc.c.cfg.logger.Logf(LogLevelInfo, "client %s request unable to be handled: %v", who, err)
			return
		}

		buf = append(buf[:0], 0, 0, 0, 0, 0, 0, 0, 0) // size (4) + correlation ID (4)
		if resp.kresp.IsFlexible() && resp.kresp.Key() != 18 {
			buf = append(buf, 0) // empty tagged fields section
		}
		buf = resp.kresp.AppendTo(buf)

		binary.BigEndian.PutUint32(buf[:4], uint32(len(buf)-4))
		binary.BigEndian.PutUint32(buf[4:8], uint32(resp.corr))

		if _, err := cc.conn.Write(buf); err != nil {
			return
		}
		if !resp.unmuted {
			cc.unmute()
		}
	}
}
