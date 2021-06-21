package miniredis

import (
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/alicebob/miniredis/v2/server"
)

const (
	msgWrongType          = "WRONGTYPE Operation against a key holding the wrong kind of value"
	msgInvalidInt         = "ERR value is not an integer or out of range"
	msgInvalidFloat       = "ERR value is not a valid float"
	msgInvalidMinMax      = "ERR min or max is not a float"
	msgInvalidRangeItem   = "ERR min or max not valid string range item"
	msgInvalidTimeout     = "ERR timeout is not a float or out of range"
	msgSyntaxError        = "ERR syntax error"
	msgKeyNotFound        = "ERR no such key"
	msgOutOfRange         = "ERR index out of range"
	msgInvalidCursor      = "ERR invalid cursor"
	msgXXandNX            = "ERR XX and NX options at the same time are not compatible"
	msgNegTimeout         = "ERR timeout is negative"
	msgInvalidSETime      = "ERR invalid expire time in set"
	msgInvalidSETEXTime   = "ERR invalid expire time in setex"
	msgInvalidPSETEXTime  = "ERR invalid expire time in psetex"
	msgInvalidKeysNumber  = "ERR Number of keys can't be greater than number of args"
	msgNegativeKeysNumber = "ERR Number of keys can't be negative"
	msgFScriptUsage       = "ERR Unknown subcommand or wrong number of arguments for '%s'. Try SCRIPT HELP."
	msgFPubsubUsage       = "ERR Unknown subcommand or wrong number of arguments for '%s'. Try PUBSUB HELP."
	msgSingleElementPair  = "ERR INCR option supports a single increment-element pair"
	msgInvalidStreamID    = "ERR Invalid stream ID specified as stream command argument"
	msgStreamIDTooSmall   = "ERR The ID specified in XADD is equal or smaller than the target stream top item"
	msgStreamIDZero       = "ERR The ID specified in XADD must be greater than 0-0"
	msgNoScriptFound      = "NOSCRIPT No matching script. Please use EVAL."
	msgUnsupportedUnit    = "ERR unsupported unit provided. please use m, km, ft, mi"
	msgNotFromScripts     = "This Redis command is not allowed from scripts"
	msgXreadUnbalanced    = "ERR Unbalanced XREAD list of streams: for each stream key an ID or '$' must be specified."
)

func errWrongNumber(cmd string) string {
	return fmt.Sprintf("ERR wrong number of arguments for '%s' command", strings.ToLower(cmd))
}

func errLuaParseError(err error) string {
	return fmt.Sprintf("ERR Error compiling script (new function): %s", err.Error())
}

// withTx wraps the non-argument-checking part of command handling code in
// transaction logic.
func withTx(
	m *Miniredis,
	c *server.Peer,
	cb txCmd,
) {
	ctx := getCtx(c)

	if ctx.nested {
		// this is a call via Lua's .call(). It's already locked.
		cb(c, ctx)
		m.signal.Broadcast()
		return
	}

	if inTx(ctx) {
		addTxCmd(ctx, cb)
		c.WriteInline("QUEUED")
		return
	}
	m.Lock()
	cb(c, ctx)
	// done, wake up anyone who waits on anything.
	m.signal.Broadcast()
	m.Unlock()
}

// blockCmd is executed returns whether it is done
type blockCmd func(*server.Peer, *connCtx) bool

// blocking keeps trying a command until the callback returns true. Calls
// onTimeout after the timeout (or when we call this in a transaction).
func blocking(
	m *Miniredis,
	c *server.Peer,
	timeout time.Duration,
	cb blockCmd,
	onTimeout func(*server.Peer),
) {
	var (
		ctx = getCtx(c)
		dl  *time.Timer
		dlc <-chan time.Time
	)
	if inTx(ctx) {
		addTxCmd(ctx, func(c *server.Peer, ctx *connCtx) {
			if !cb(c, ctx) {
				onTimeout(c)
			}
		})
		c.WriteInline("QUEUED")
		return
	}
	if timeout != 0 {
		dl = time.NewTimer(timeout)
		defer dl.Stop()
		dlc = dl.C
	}

	m.Lock()
	defer m.Unlock()
	for {
		done := cb(c, ctx)
		if done {
			return
		}
		// there is no cond.WaitTimeout(), so hence the the goroutine to wait
		// for a timeout
		var (
			wg     sync.WaitGroup
			wakeup = make(chan struct{}, 1)
		)
		wg.Add(1)
		go func() {
			m.signal.Wait()
			wakeup <- struct{}{}
			wg.Done()
		}()
		select {
		case <-wakeup:
		case <-dlc:
			onTimeout(c)
			m.signal.Broadcast() // to kill the wakeup go routine
			wg.Wait()
			return
		case <-m.Ctx.Done():
			m.signal.Broadcast() // to kill the wakeup go routine
			wg.Wait()
			return
		}
		wg.Wait()
	}
}

// formatBig formats a float the way redis does
func formatBig(v *big.Float) string {
	// Format with %f and strip trailing 0s.
	if v.IsInf() {
		return "inf"
	}
	// if math.IsInf(v, -1) {
	// return "-inf"
	// }
	return stripZeros(fmt.Sprintf("%.17f", v))
}

func stripZeros(sv string) string {
	for strings.Contains(sv, ".") {
		if sv[len(sv)-1] != '0' {
			break
		}
		// Remove trailing 0s.
		sv = sv[:len(sv)-1]
		// Ends with a '.'.
		if sv[len(sv)-1] == '.' {
			sv = sv[:len(sv)-1]
			break
		}
	}
	return sv
}

// redisRange gives Go offsets for something l long with start/end in
// Redis semantics. Both start and end can be negative.
// Used for string range and list range things.
// The results can be used as: v[start:end]
// Note that GETRANGE (on a string key) never returns an empty string when end
// is a large negative number.
func redisRange(l, start, end int, stringSymantics bool) (int, int) {
	if start < 0 {
		start = l + start
		if start < 0 {
			start = 0
		}
	}
	if start > l {
		start = l
	}

	if end < 0 {
		end = l + end
		if end < 0 {
			end = -1
			if stringSymantics {
				end = 0
			}
		}
	}
	end++ // end argument is inclusive in Redis.
	if end > l {
		end = l
	}

	if end < start {
		return 0, 0
	}
	return start, end
}
