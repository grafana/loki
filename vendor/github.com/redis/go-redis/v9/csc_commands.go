package redis

import (
	"bytes"
	"strconv"
	"strings"
	"sync"

	"github.com/redis/go-redis/v9/internal/proto"
)

// defaultCacheableCommands is the allow-list of read-only, deterministic
// commands whose responses may be stored in the client-side cache. Keys are
// lowercase to match baseCmd.Name() on the hot path.
var defaultCacheableCommands = map[string]struct{}{
	// String commands
	"get": {}, "mget": {}, "getbit": {}, "getrange": {},
	"strlen": {}, "substr": {},
	// Hash commands
	"hget": {}, "hgetall": {}, "hmget": {},
	"hkeys": {}, "hvals": {}, "hlen": {},
	"hexists": {}, "hstrlen": {},
	// List commands
	"lindex": {}, "llen": {}, "lpos": {}, "lrange": {},
	// Set commands
	"scard": {}, "sismember": {}, "smembers": {}, "smismember": {},
	"sdiff": {}, "sinter": {}, "sintercard": {}, "sunion": {},
	// Sorted-set commands
	"zcard": {}, "zcount": {}, "zlexcount": {}, "zmscore": {},
	"zrange": {}, "zrangebylex": {}, "zrangebyscore": {},
	"zrank": {}, "zrevrange": {}, "zrevrangebylex": {},
	"zrevrangebyscore": {}, "zrevrank": {}, "zscore": {},
	"zdiff": {}, "zinter": {}, "zunion": {},
	// Bit commands
	"bitcount": {}, "bitfield_ro": {}, "bitpos": {},
	// Key/generic commands
	"exists": {}, "type": {}, "sort_ro": {}, "lcs": {},
	// Geo commands
	"geodist": {}, "geohash": {}, "geopos": {}, "geosearch": {},
	"georadiusbymember_ro": {}, "georadius_ro": {},
	// Stream commands. XREAD is deliberately excluded: it supports BLOCK, and
	// its $/+ IDs are state-relative, so identical args are not deterministic.
	// XPENDING is excluded for the same class of reason: its extended form
	// returns wall-clock-relative idle times and its IDLE filter is
	// time-dependent, so identical args yield different correct results with
	// no key modification (and therefore no invalidation).
	"xlen": {}, "xrange": {}, "xrevrange": {},
	// JSON (RedisJSON) commands
	"json.get": {}, "json.mget": {}, "json.arrindex": {}, "json.arrlen": {},
	"json.objkeys": {}, "json.objlen": {}, "json.resp": {},
	"json.strlen": {}, "json.type": {},
	// TimeSeries commands
	"ts.get": {}, "ts.info": {}, "ts.range": {}, "ts.revrange": {},
}

// isCacheable reports whether cmd is eligible for client-side caching: its
// name is on the allow-list and it operates on at least one key.
func isCacheable(cmd Cmder) bool {
	// Commands such as RawWriteToCmd stream replies directly to an io.Writer.
	// Capturing their replies for CSC would buffer the entire response first,
	// defeating their streaming and allocation guarantees.
	if cmd.NoRetry() {
		return false
	}
	if _, ok := defaultCacheableCommands[cmd.Name()]; !ok {
		return false
	}
	// SORT_RO ... BY/GET reads pattern keys that extractRedisKeys can't
	// enumerate, so its invalidations would be dropped and the result go stale.
	// Plain SORT_RO is fine.
	if cmd.Name() == "sort_ro" && sortROHasByGet(cmd) {
		return false
	}
	return cmdFirstKeyPosWithInfo(cmd, nil) > 0
}

// sortROHasByGet reports whether a SORT_RO invocation uses BY or GET
// (case-insensitive), scanning past the command name and key. stringArg
// normalizes string, *string, and []byte tokens.
func sortROHasByGet(cmd Cmder) bool {
	for i := 2; i < len(cmd.Args()); i++ {
		if s := cmd.stringArg(i); strings.EqualFold(s, "by") || strings.EqualFold(s, "get") {
			return true
		}
	}
	return false
}

// isClientTrackingCmd reports whether cmd is a CLIENT TRACKING subcommand (any
// mode: ON, OFF, or with options). Name and stringArg normalize string,
// *string, and []byte arguments.
func isClientTrackingCmd(cmd Cmder) bool {
	return cmd.Name() == "client" && strings.EqualFold(cmd.stringArg(1), "tracking")
}

// isSelectCmd reports whether cmd changes the selected database on its
// connection. CSC keys are namespaced with Options.DB, so a runtime SELECT
// would make the connection's actual database diverge from the cache namespace.
func isSelectCmd(cmd Cmder) bool {
	return cmd.Name() == "select"
}

// isAuthCmd reports whether cmd changes the authenticated user on its
// connection. The cache namespace is fixed from Options.Username, so runtime
// authentication would make the connection identity diverge from it.
func isAuthCmd(cmd Cmder) bool {
	return cmd.Name() == "auth"
}

// isProtocolChangingHelloCmd reports whether HELLO includes a protocol version
// (and can therefore switch a tracked RESP3 connection to RESP2). A bare HELLO
// only reports connection properties and is safe.
func isProtocolChangingHelloCmd(cmd Cmder) bool {
	return cmd.Name() == "hello" && len(cmd.Args()) > 1
}

// isResetCmd reports whether cmd resets all server-side connection state.
// RESET disables tracking, switches to RESP2, deauthenticates, and changes
// other state that a pooled CSC connection relies on.
func isResetCmd(cmd Cmder) bool {
	return cmd.Name() == "reset"
}

// isSubscribeCmd reports whether a raw command would turn an ordinary pooled
// connection into a Pub/Sub connection. Pub/Sub pushes are deliberately left
// for the dedicated PubSub reader, so the CSC drainer cannot safely own such a
// connection.
func isSubscribeCmd(cmd Cmder) bool {
	switch cmd.Name() {
	case "subscribe", "psubscribe", "ssubscribe":
		return true
	default:
		return false
	}
}

// buildCacheKey returns the RESP-encoded form of the command's argument list,
// used as a collision-free canonical cache key. ok is false when the writer
// cannot marshal the arguments, in which case the caller must skip caching
// rather than bucket the command under an empty key.
// (The un-namespaced form is only needed by tests; it lives in
// csc_cachekey_test.go so production code has a single entry point.)

// cacheKeyScratch is the reusable buffer+writer pair buildCacheKeyNS encodes
// into. Building a cache key was the single largest allocator on the cached
// read path -- 32% of all bytes allocated -- because every call heap-allocated
// a bytes.Buffer, a proto.Writer (which itself allocates two 64-byte scratch
// slices), grew the buffer, and then copied it out with String(). Only the
// final String() is inherent: the key is retained, the scratch is not.
type cacheKeyScratch struct {
	buf bytes.Buffer
	wr  *proto.Writer
}

var cacheKeyScratchPool = sync.Pool{
	New: func() any {
		s := &cacheKeyScratch{}
		s.wr = proto.NewWriter(&s.buf)
		return s
	},
}

// buildCacheKeyNS renders cmd's RESP encoding prefixed by ns, in ONE
// allocation (the returned string).
//
// Fusing the namespace in matters as much as pooling: the caller used to take
// this function's result and then do prefix+key, a second full copy of every
// cache key on every cached read. Writing the prefix into the buffer first
// makes the single String() produce the namespaced key directly, and the
// suffix after ns is still exactly the wire encoding -- which the refresher
// relies on when it slices the namespace back off to re-issue the command.
func buildCacheKeyNS(cmd Cmder, ns string) (string, bool) {
	args := cmd.Args()
	if len(args) == 0 {
		return "", false
	}
	s := cacheKeyScratchPool.Get().(*cacheKeyScratch)
	defer func() {
		// Drop an oversized buffer instead of pooling it, so one huge command
		// does not pin that capacity for the process lifetime.
		if s.buf.Cap() > cacheKeyScratchMaxCap {
			return
		}
		s.buf.Reset()
		cacheKeyScratchPool.Put(s)
	}()
	s.buf.Reset()
	s.buf.WriteString(ns)
	// The pooled writer targets s.buf for its whole life; Reset here only
	// guards against a zero-value scratch reaching this path.
	if s.wr == nil {
		s.wr = proto.NewWriter(&s.buf)
	}
	if err := s.wr.WriteArgs(args); err != nil {
		return "", false
	}
	return s.buf.String(), true
}

// cacheKeyScratchMaxCap bounds the buffer capacity kept in the pool.
const cacheKeyScratchMaxCap = 64 << 10

// isWireKeyType reports whether a key argument of v's type renders, through
// stringArg, exactly as proto.Writer sends it to the server, so invalidation
// lookups match the key names in the server's "invalidate" pushes. Only
// byte-identical types are accepted (fmt.Sprint of any integer matches the
// writer's base-10 strconv output); for anything else — pointers, bools,
// times, durations, floats, BinaryMarshaler values — the rendering can
// diverge, the invalidation would never match, and the entry would be served
// stale forever, so the caller skips caching (see processCached).
func isWireKeyType(v any) bool {
	switch v.(type) {
	case string, []byte,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return true
	}
	return false
}

// cscKeySpan returns the inclusive argument range [lo, hi] that holds cmd's
// Redis keys. For every cacheable command shape the keys are contiguous.
// ok is false when cmd has no key.
func cscKeySpan(cmd Cmder) (lo, hi int, ok bool) {
	firstKey := cmdFirstKeyPosWithInfo(cmd, nil)
	// SetFirstKeyPos is public and takes any int8, so a negative position is
	// possible. It is not a key: reject it here rather than index args with it.
	if firstKey <= 0 {
		return 0, 0, false
	}
	argsLen := len(cmd.Args())
	if firstKey >= argsLen {
		return 0, 0, false
	}

	switch cmd.Name() {
	// All remaining args from firstKeyPos are keys.
	case "mget", "exists", "sdiff", "sinter", "sunion":
		return firstKey, argsLen - 1, true

	// Numkeys pattern: numkeys at args[1], keys from args[2].
	case "sintercard", "zdiff", "zinter", "zunion":
		if argsLen < 3 {
			return 0, 0, false
		}
		numKeys, ok := cscNumKeys(cmd)
		if !ok || numKeys <= 0 {
			return 0, 0, false
		}
		return 2, min(2+numKeys, argsLen) - 1, true

	// LCS: exactly two consecutive keys starting at firstKeyPos.
	case "lcs":
		if firstKey+1 >= argsLen {
			return 0, 0, false
		}
		return firstKey, firstKey + 1, true

	// JSON.MGET: keys from firstKeyPos to second-to-last (last arg is the
	// JSON path, not a key).
	case "json.mget":
		if argsLen-2 < firstKey {
			return 0, 0, false
		}
		return firstKey, argsLen - 2, true
	}

	// Single key at firstKeyPos (GET, HGET, LRANGE, ...).
	return firstKey, firstKey, true
}

// cscNumKeys parses the numkeys argument (args[1]). The typed commands pass
// an int, which is read without rendering it to a string.
func cscNumKeys(cmd Cmder) (int, bool) {
	switch v := cmd.Args()[1].(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	}
	n, err := strconv.Atoi(cmd.stringArg(1))
	return n, err == nil
}

// cscKeysRenderable reports whether every key in [lo, hi] is a type
// isWireKeyType accepts. It allocates nothing, so processCached runs it on every read,
// before it builds the cache key.
func cscKeysRenderable(cmd Cmder, lo, hi int) bool {
	args := cmd.Args()
	for i := lo; i <= hi; i++ {
		if !isWireKeyType(args[i]) {
			return false
		}
	}
	return true
}
