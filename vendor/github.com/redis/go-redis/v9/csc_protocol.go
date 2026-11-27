package redis

import (
	"context"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9/internal"
	"github.com/redis/go-redis/v9/internal/pool"
	"github.com/redis/go-redis/v9/internal/proto"
)

var (
	cscYesWire = []byte("*3\r\n$6\r\nCLIENT\r\n$7\r\nCACHING\r\n$3\r\nYES\r\n")
	cscNoWire  = []byte("*3\r\n$6\r\nCLIENT\r\n$7\r\nCACHING\r\n$2\r\nNO\r\n")
)

func (c *baseClient) cscFlag(cmd Cmder, store bool) byte {
	// ASK redirects must not carry a caching prefix. OPTIN YES still requires
	// live client-level CSC; OPTOUT NO is decided by the caller against the
	// connection's init generation so already-tracked sockets keep receiving
	// NO after a mid-life disableCSCServing (see #4236 item 1).
	if cmd.cacheState().has(cscAsk) {
		return 0
	}
	if c.cscPolicy.mode == TrackingModeOptIn && store && c.cscTrackingRequested() {
		return 'Y'
	}
	if c.cscPolicy.mode == TrackingModeOptOut && !store && c.cscTrackable(cmd) {
		return 'N'
	}
	return 0
}

func writeCSCPrefix(wr *proto.Writer, flag byte) error {
	var err error
	if flag == 'Y' {
		_, err = wr.Write(cscYesWire)
	}
	if flag == 'N' {
		_, err = wr.Write(cscNoWire)
	}
	return err
}

// readCSCAck consumes one complete control reply. A Redis error is recoverable:
// the following data reply still belongs to this request, but cannot publish.
// Everything else must establish the exact OK frame or retire the connection.
func (c *baseClient) readCSCAck(ctx context.Context, cn *pool.Conn, rd *proto.Reader) (bool, error) {
	if err := c.drainPushFrames(ctx, cn, rd, true); err != nil {
		return false, err
	}
	typ, err := rd.PeekReplyType()
	if err != nil {
		return false, err
	}
	if typ != proto.RespStatus && typ != proto.RespError && typ != proto.RespBlobError {
		return false, fmt.Errorf("redis: invalid CLIENT CACHING reply type %q", typ)
	}
	status, err := rd.ReadString()
	if isRedisError(err) {
		internal.Logger.Printf(ctx, "csc: CLIENT CACHING rejected: %v; returning uncached data", err)
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if status != "OK" {
		return false, fmt.Errorf("redis: invalid CLIENT CACHING status %q", status)
	}
	return true, nil
}

// cscPipelineFlags disables tracking for uncached reads when a pipeline uses
// a tracked main-pool connection. Dedicated pipeline connections are untracked.
// Capture the flags once so metadata refresh cannot change the reply count.
func (c *baseClient) cscPipelineFlags(cn *pool.Conn, cmds []Cmder) []byte {
	// Gate on the connection's init generation, not live client-level CSC: a
	// mid-life disable must still prefix uncached OPTOUT commands on sockets
	// that were initialized with TRACKING ON (#4236 item 1).
	if c.cscPolicy.mode != TrackingModeOptOut || c.cscConnInitGen(cn.GetID()) == 0 {
		return nil
	}
	var flags []byte
	multi := -1
	var asking, askingTx bool
	for i, cmd := range cmds {
		// Raw pipelines can contain complete transactions anywhere in the
		// batch. A prefix inside MULTI would be queued and change EXEC's reply.
		name, _ := cscCommandToken(cmd, 0)
		afterAsking := asking
		asking = len(cmd.Args()) == 1 && strings.EqualFold(name, "asking")
		if len(cmd.Args()) == 1 {
			switch internal.ToLower(name) {
			case "multi":
				// A rejected nested MULTI does not end the outer transaction.
				if multi < 0 {
					multi, askingTx = i, afterAsking
				}
				continue
			case "exec", "discard":
				multi, askingTx = -1, false
				continue
			}
		}
		// Never insert between ASKING and its command, including a transaction.
		if askingTx || (multi < 0 && afterAsking) {
			continue
		}
		if c.cscFlag(cmd, false) != 'N' {
			continue
		}
		if flags == nil {
			flags = make([]byte, len(cmds))
		}
		if multi >= 0 {
			// Redis preserves the CACHING flag across MULTI/EXEC: NO before
			// MULTI suppresses tracking for the entire transaction.
			flags[multi] = 'N'
		} else {
			flags[i] = 'N'
		}
	}
	return flags
}

func writeCSCPipelineCmds(wr *proto.Writer, cmds []Cmder, flags []byte) error {
	if flags == nil {
		return writeCmds(wr, cmds)
	}
	for i, cmd := range cmds {
		if err := writeCSCPrefix(wr, flags[i]); err != nil {
			return err
		}
		if err := writeCmd(wr, cmd); err != nil {
			return err
		}
	}
	return nil
}

func (c *baseClient) readCSCPipelinePrefix(ctx context.Context, cn *pool.Conn, rd *proto.Reader) error {
	_, err := c.readCSCAck(ctx, cn, rd)
	if err == nil {
		err = c.drainPushFrames(ctx, cn, rd, true)
	}
	if err != nil {
		_ = cn.Close()
		return fmt.Errorf("%w: pipeline CLIENT CACHING reply: %w", errConnUnusable, err)
	}
	return nil
}
