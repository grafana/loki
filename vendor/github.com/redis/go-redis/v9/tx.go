package redis

import (
	"context"
	"errors"

	"github.com/redis/go-redis/v9/internal/pool"
	"github.com/redis/go-redis/v9/internal/proto"
)

// TxFailedErr transaction redis failed.
const TxFailedErr = proto.RedisError("redis: transaction failed")

// Tx implements Redis transactions as described in
// https://redis.io/docs/latest/develop/using-commands/transactions. It's NOT safe for concurrent use
// by multiple goroutines, because Exec resets list of watched keys.
//
// If you don't need WATCH, use Pipeline instead.
type Tx struct {
	baseClient
	cmdable
	statefulCmdable

	// watchArmed reports whether a WATCH issued through Tx.Watch may still be
	// active on the connection. It is set on a successful WATCH and cleared once
	// the watched keys are discarded server-side: on UNWATCH (Tx.Unwatch) and on
	// EXEC, including an aborted EXEC (the TxPipeline closure). Close uses it to
	// skip an otherwise redundant UNWATCH round trip.
	//
	// Only Tx.Watch, Tx.Unwatch and the TxPipeline EXEC closure maintain this
	// flag. go-redis never issues a standalone DISCARD (Pipeline.Discard is a
	// client-side buffer reset, not a server command). Issuing a WATCH directly
	// via Process bypasses tracking, so Close would not release it and the watch
	// would leak onto the pooled connection; that is the one case where skipping
	// UNWATCH is unsafe, and using raw Process for WATCH/EXEC/UNWATCH is therefore
	// unsupported.
	//
	// Tx is not safe for concurrent use (see above), so watchArmed is accessed
	// only from the goroutine that owns the Tx and needs no synchronization.
	watchArmed bool
}

func (c *Client) newTx() *Tx {
	dialPool, _ := c.connPool.(*pool.ConnPool)
	tx := Tx{
		baseClient: baseClient{
			fixedDialEndpoint:  c.fixedDialEndpoint,
			dialExhaustionPool: dialPool,
			opt:                c.cloneOpt(), // Clone options under optLock to avoid race with initConn
			rec:                c.rec,
			connPool:           c.baseClient.newStickyConnPool(),
			hooksMixin:         c.hooksMixin.clone(),
			pushProcessor:      c.pushProcessor, // Copy push processor from parent client
			onClose:            &onCloseHooks{},
			// Share the HIMPORT fieldset registry: the sticky pool borrows
			// connections from the parent client's pool, so fieldsets
			// prepared on them stay valid after the connections are
			// returned.
			himport: c.himport,
			// Carry the shared eviction hook (not csc: a sticky Tx must not serve
			// cached reads) so close/reinit hooks on a Watch-initialized conn still
			// evict from the parent cache.
			cscPoolHook: c.cscPoolHook,
			cscActive:   c.cscActive,
			cscPolicy:   c.cscPolicy,
			// Carry the parent's streaming-credentials manager so initConn can
			// create a credentials listener when Watch initializes a fresh
			// connection (issue #4154).
			streamingCredentialsManager: c.streamingCredentialsManager,
		},
	}
	// Tx reads are uncached but still OPTOUT-prefix when trackable; share the
	// parent's CommandMetadata overrides for custom read-only keyed commands.
	c.baseClient.shareCommandMetadata(&tx.baseClient)
	tx.init()
	return &tx
}

func (c *Tx) init() {
	c.cmdable = c.Process
	c.statefulCmdable = c.Process

	c.initHooks(hooks{
		dial:       c.baseClient.dial,
		process:    c.baseClient.process,
		pipeline:   c.baseClient.processPipeline,
		txPipeline: c.baseClient.processTxPipeline,
	})
}

func (c *Tx) Process(ctx context.Context, cmd Cmder) error {
	err := c.processHook(ctx, cmd)
	// Same as Client.Process: keep the serving-endpoint wrapper off cmd.Err().
	cmd.SetErr(unwrapServingEndpoint(err))
	return err
}

// Watch prepares a transaction and marks the keys to be watched
// for conditional execution if there are any keys.
//
// The transaction is automatically closed when fn exits.
func (c *Client) Watch(ctx context.Context, fn func(*Tx) error, keys ...string) error {
	tx := c.newTx()
	defer tx.Close(ctx)
	if len(keys) > 0 {
		// Return the Process error, not cmd.Err(): it keeps the serving
		// endpoint that ClusterClient.Watch needs for a hostless redirect.
		if _, err := tx.watch(ctx, keys...); err != nil {
			return err
		}
	}
	return fn(tx)
}

// Close closes the transaction, releasing any open resources.
func (c *Tx) Close(ctx context.Context) error {
	// UNWATCH is only needed while a WATCH is still active. EXEC discards the
	// watched keys server-side on both commit and abort, so the common
	// WATCH/.../EXEC paths leave nothing to release and avoid the extra round
	// trip.
	if c.watchArmed {
		// Cleanup must not inherit the caller's cache intent.
		_ = c.Unwatch(cscUnmarkedContext(ctx)).Err()
		if c.watchArmed {
			// A refused or queued UNWATCH is a complete reply, but does not
			// prove that the connection is safe for another borrower. Remove
			// the pinned connection without replaying cleanup or changing the
			// callback's error. Get cannot dial here: WATCH already pinned it.
			if cn, err := c.connPool.Get(ctx); err == nil {
				c.connPool.Remove(ctx, cn, errors.New("redis: UNWATCH did not clear transaction state"))
			}
		}
	}
	return c.baseClient.Close()
}

// Watch marks the keys to be watched for conditional execution
// of a transaction.
func (c *Tx) Watch(ctx context.Context, keys ...string) *StatusCmd {
	cmd, _ := c.watch(ctx, keys...)
	return cmd
}

// watch is Watch that also returns the Process error, which (unlike cmd.Err())
// keeps the serving-endpoint wrapper.
func (c *Tx) watch(ctx context.Context, keys ...string) (*StatusCmd, error) {
	args := make([]interface{}, 1+len(keys))
	args[0] = "watch"
	for i, key := range keys {
		args[1+i] = key
	}
	cmd := NewStatusCmd(ctx, args...)
	err := c.Process(ctx, cmd)
	// A successful WATCH leaves keys watched on the connection that Close must
	// later release with UNWATCH.
	if cmd.Err() == nil {
		c.watchArmed = true
	}
	return cmd, err
}

// Unwatch flushes all the previously watched keys for a transaction.
func (c *Tx) Unwatch(ctx context.Context, keys ...string) *StatusCmd {
	args := make([]interface{}, 1+len(keys))
	args[0] = "unwatch"
	for i, key := range keys {
		args[1+i] = key
	}
	cmd := NewStatusCmd(ctx, args...)
	_ = c.Process(ctx, cmd)
	// The watched keys have been released, so Close need not UNWATCH again.
	if cmd.Err() == nil && cmd.Val() == "OK" {
		c.watchArmed = false
	}
	return cmd
}

// Pipeline creates a pipeline. Usually it is more convenient to use Pipelined.
func (c *Tx) Pipeline() Pipeliner {
	pipe := Pipeline{
		exec: func(ctx context.Context, cmds []Cmder) error {
			return c.processPipelineHook(ctx, cmds)
		},
	}
	pipe.init()
	return &pipe
}

// Pipelined executes commands queued in the fn outside of the transaction.
// Use TxPipelined if you need transactional behavior.
func (c *Tx) Pipelined(ctx context.Context, fn func(Pipeliner) error) ([]Cmder, error) {
	return c.Pipeline().Pipelined(ctx, fn)
}

// TxPipelined executes commands queued in the fn in the transaction.
//
// When using WATCH, EXEC will execute commands only if the watched keys
// were not modified, allowing for a check-and-set mechanism.
//
// Exec always returns list of commands. If transaction fails
// TxFailedErr is returned. Otherwise Exec returns an error of the first
// failed command or nil.
func (c *Tx) TxPipelined(ctx context.Context, fn func(Pipeliner) error) ([]Cmder, error) {
	return c.TxPipeline().Pipelined(ctx, fn)
}

// TxPipeline creates a pipeline. Usually it is more convenient to use TxPipelined.
func (c *Tx) TxPipeline() Pipeliner {
	pipe := Pipeline{
		exec: func(ctx context.Context, cmds []Cmder) error {
			cmds = wrapMultiExec(ctx, cmds)
			err := c.processTxPipelineHook(ctx, cmds)
			// EXEC discards the watched keys server-side, so the watch is
			// cleared only when EXEC actually ran: a nil error (committed),
			// TxFailedErr (a watched key changed, EXEC returned nil), or an
			// EXECABORT error (a queued command was rejected, so EXEC discarded
			// the transaction). All three release the watched keys. Any other
			// error may be reported before EXEC executes (for example -LOADING on
			// the MULTI reply) or on a broken connection, so leave watchArmed set
			// and let Close send UNWATCH rather than risk leaving a watch on a
			// pooled connection.
			if err == nil || errors.Is(err, TxFailedErr) || IsExecAbortError(err) {
				c.watchArmed = false
			}
			return err
		},
	}
	pipe.init()
	return &pipe
}

func wrapMultiExec(ctx context.Context, cmds []Cmder) []Cmder {
	if len(cmds) == 0 {
		panic("not reached")
	}
	cmdsCopy := make([]Cmder, len(cmds)+2)
	cmdsCopy[0] = NewStatusCmd(ctx, "multi")
	copy(cmdsCopy[1:], cmds)
	cmdsCopy[len(cmdsCopy)-1] = NewSliceCmd(ctx, "exec")
	return cmdsCopy
}
