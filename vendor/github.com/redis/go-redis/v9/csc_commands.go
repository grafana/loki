package redis

import (
	"bytes"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/redis/go-redis/v9/internal"
	"github.com/redis/go-redis/v9/internal/proto"
)

// CSC eligibility: a command may be cached only when its COMMAND metadata
// shows it is read-only, keyed, deterministic, non-blocking, not a script
// runner, and not marked dont_cache; unknown commands are never cached.
// Records resolve into a commandMetadataView (see command_metadata.go), and
// every invocation reads exactly one view.

//go:generate go run ./internal/cmdmetagen -addr localhost:6379 -out command_info_snapshot.go

// cscCmdBits mirrors the COMMAND flags/tips the eligibility check uses.
type cscCmdBits uint8

const (
	cscFlagReadonly cscCmdBits = 1 << iota
	cscFlagScriptRunner
	cscFlagBlocking
	cscTipNondeterministicOutput
	cscTipDontCache
	// Set only for COMPLETE key specs; incomplete/not_key specs can't prove
	// which keys the command reads.
	cscHasKeySpec
)

// cscKeyExtract: how key arguments are located. None = keys can't be listed
// reliably, so the command is never cached even if eligible.
type cscKeyExtract uint8

const (
	cscKeyExtractNone cscKeyExtract = iota
	// firstKey..lastKey every step args; negative lastKey counts from the end.
	cscKeyExtractRange
	// The arg at numkeysAt holds the key count; keys start at firstKey.
	cscKeyExtractKeynum
)

// cscInvocationGuard identifies argument checks that COMMAND cannot express.
type cscInvocationGuard uint8

const (
	cscInvocationGuardNone cscInvocationGuard = iota
	cscInvocationGuardSortRONoByGet
)

type cscCommandMeta struct {
	bits    cscCmdBits
	extract cscKeyExtract
	guard   cscInvocationGuard

	firstKey  int16
	lastKey   int16
	step      int16
	numkeysAt int16
}

// commandMetadataCorrection patches a verified server metadata gap before CSC
// and Cluster routing derive their decisions.
type commandMetadataCorrection struct {
	tips        []string
	removeFlags []string

	keySpecs    []KeySpec
	firstKeyPos int8
	lastKeyPos  int8
	stepCount   int8
}

// commandMetadataCorrections contains verified server metadata fixes shared by
// CSC and routing. Remove entries only after probing the server fix.
var commandMetadataCorrections = map[string]commandMetadataCorrection{
	// Read-only on paper, but its purpose is to bump key LRU/LFU state.
	"touch": {tips: []string{"dont_cache"}},

	// Random output; the server doesn't tag it yet (its siblings are tagged).
	"vrandmember": {tips: []string{"nondeterministic_output"}},

	// Marked readonly but rewrites the filter; disable caching and replica routing.
	"cf.compact": {tips: []string{"dont_cache"}, removeFlags: []string{"readonly"}},

	// The server reports and tracks only the first of N keys. Keep caching
	// disabled and correct the routing range (N keys followed by a path).
	"json.mget": {
		tips: []string{"dont_cache"},
		keySpecs: []KeySpec{{
			Flags: []string{"RO", "access"}, BeginSearch: "index", Index: 1,
			FindKeys: "range", LastKey: -2, KeyStep: 1,
		}},
		firstKeyPos: 1, lastKeyPos: -2, stepCount: 1,
	},

	// The server registers no tracking for any of their series: writes never
	// send an invalidation.
	"ts.nrange":    {tips: []string{"dont_cache"}},
	"ts.nrevrange": {tips: []string{"dont_cache"}},

	// Replies include consumer-group state, but group changes (XGROUP
	// CREATE/SETID) never invalidate the stream key.
	"xinfo|stream": {tips: []string{"dont_cache"}},
	"xinfo|groups": {tips: []string{"dont_cache"}},

	// Usage changes on mutations that never signal the key (e.g. XGROUP CREATE).
	"memory|usage": {tips: []string{"dont_cache"}},
}

// cscNegativeBits are the bits that each rule out caching.
const cscNegativeBits = cscFlagScriptRunner | cscFlagBlocking |
	cscTipNondeterministicOutput | cscTipDontCache

// cscExtractionOverride handles invocation-level extraction rules that COMMAND
// cannot express.
type cscExtractionOverride struct {
	extraKeySpecs cscExtraKeySpecs
	guard         cscInvocationGuard
}

type cscExtraKeySpecs uint8

const (
	cscExtraKeySpecsNone cscExtraKeySpecs = iota
	cscExtraKeySpecsSortROPattern
)

var cscExtractionOverrides = map[string]cscExtractionOverride{
	// BY/GET keys are pattern-derived; legacy positions are safe only without them.
	"sort_ro": {
		extraKeySpecs: cscExtraKeySpecsSortROPattern,
		guard:         cscInvocationGuardSortRONoByGet,
	},
}

func (kind cscExtraKeySpecs) permits(info *CommandInfo, complete, other int) bool {
	switch kind {
	case cscExtraKeySpecsSortROPattern:
		if len(info.KeySpecs) != 2 || complete != 1 || other != 1 {
			return false
		}
		for _, spec := range info.KeySpecs {
			if cscSpecComplete(spec) {
				continue
			}
			if spec.BeginSearch != "unknown" || spec.FindKeys != "unknown" {
				return false
			}
			if len(spec.Flags) != 2 {
				return false
			}
			var readOnly, access bool
			for _, flag := range spec.Flags {
				switch flag {
				case "RO":
					readOnly = true
				case "access":
					access = true
				default:
					return false
				}
			}
			if !readOnly || !access {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// cscSpecComplete reports whether a spec identifies exact Redis keys.
func cscSpecComplete(ks KeySpec) bool {
	if (ks.BeginSearch != "index" && ks.BeginSearch != "keyword") ||
		(ks.FindKeys != "range" && ks.FindKeys != "keynum") {
		return false
	}
	return classifyCommandMetadataKeySpecFlags(ks.Flags).cscComplete
}

// cscDeriveMeta compresses a record into the hot-path form. Extraction is
// emitted only when key positions are unambiguous (at most one complete spec:
// a partial key list would mean dropped invalidations and stale entries).
func cscDeriveMeta(info *CommandInfo) cscCommandMeta {
	var meta cscCommandMeta
	override := cscExtractionOverrides[internal.ToLower(info.Name)]
	meta.guard = override.guard

	// The readonly FLAG is the normative source; the ReadOnly convenience
	// bool is deliberately ignored.
	var moduleCmd, movableKeys bool
	for _, f := range info.Flags {
		switch f {
		case "readonly":
			meta.bits |= cscFlagReadonly
		case "script_runner":
			meta.bits |= cscFlagScriptRunner
		case "blocking":
			meta.bits |= cscFlagBlocking
		case "module":
			moduleCmd = true
		case "movablekeys":
			movableKeys = true
		}
	}
	for _, t := range info.Tips {
		switch t {
		case "nondeterministic_output":
			meta.bits |= cscTipNondeterministicOutput
		case "dont_cache":
			meta.bits |= cscTipDontCache
		}
	}

	var complete, other int
	var completeSpec KeySpec
	for _, ks := range info.KeySpecs {
		if cscSpecComplete(ks) {
			complete++
			completeSpec = ks
		} else {
			other++
		}
	}
	if complete > 0 {
		meta.bits |= cscHasKeySpec
	}

	// Modern key specs are authoritative; never fall back when any are present.
	if len(info.KeySpecs) == 0 {
		meta.firstKey = int16(info.FirstKeyPos)
		meta.lastKey = int16(info.LastKeyPos)
		meta.step = int16(info.StepCount)
	}

	hasOverride := override.extraKeySpecs.permits(info, complete, other)
	canExtract := (other == 0 && complete <= 1) || hasOverride
	if !canExtract {
		return meta
	}

	// Invalidation follows the positions getKeysFromCommand() reports: the
	// module getkeys API, then getkeys_proc, then the legacy range; never a key
	// spec. A module command that declares movable keys may register no getkeys
	// callback and is then tracked by its legacy positions alone, usually none.
	// Its spec positions cannot be checked against what the server tracks, so
	// without an override vouching for the shape it must not be cached.
	if moduleCmd && movableKeys && !hasOverride {
		return meta
	}

	if complete == 1 && completeSpec.BeginSearch == "index" {
		switch completeSpec.FindKeys {
		case "range":
			if completeSpec.Index < 1 || completeSpec.Index > math.MaxInt16 ||
				completeSpec.KeyStep < 1 || completeSpec.KeyStep > math.MaxInt16 ||
				completeSpec.Limit != 0 || completeSpec.LastKey < math.MinInt16 {
				return meta
			}
			lastKey := completeSpec.LastKey
			if lastKey >= 0 {
				absoluteLast := int64(completeSpec.Index) + int64(lastKey)
				if absoluteLast > math.MaxInt16 {
					return meta
				}
				lastKey = int(absoluteLast)
			}
			meta.extract = cscKeyExtractRange
			meta.firstKey = int16(completeSpec.Index)
			meta.lastKey = int16(lastKey)
			meta.step = int16(completeSpec.KeyStep)
		case "keynum":
			// Validate operands before addition to prevent offset cancellation.
			if completeSpec.Index < 1 || completeSpec.Index > math.MaxInt16 ||
				completeSpec.KeyNumIdx < 0 || completeSpec.KeyNumIdx > math.MaxInt16 ||
				completeSpec.FirstKey < 1 || completeSpec.FirstKey > math.MaxInt16 ||
				completeSpec.KeyStep < 1 || completeSpec.KeyStep > math.MaxInt16 {
				return meta // malformed positions: no extraction, never cached
			}
			numkeysAt := int64(completeSpec.Index) + int64(completeSpec.KeyNumIdx)
			firstKey := int64(completeSpec.Index) + int64(completeSpec.FirstKey)
			if numkeysAt < 1 || numkeysAt > math.MaxInt16 ||
				firstKey < 1 || firstKey > math.MaxInt16 {
				return meta // malformed positions: no extraction, never cached
			}
			meta.extract = cscKeyExtractKeynum
			meta.numkeysAt = int16(numkeysAt)
			meta.firstKey = int16(firstKey)
			meta.step = int16(completeSpec.KeyStep)
		}
		return meta
	}

	if len(info.KeySpecs) == 0 && info.FirstKeyPos > 0 && info.StepCount > 0 {
		meta.extract = cscKeyExtractRange
	}
	return meta
}

// cscLookupMeta resolves cmd's metadata in view; ok is false for unknown
// commands. Name and subcommand tokens must be wire-faithful (see
// cscWireFaithfulText).
func cscLookupMeta(view *commandMetadataView, cmd Cmder) (cscCommandMeta, bool) {
	args := cmd.Args()
	if len(args) == 0 || !cscWireFaithfulText(args[0]) {
		return cscCommandMeta{}, false
	}
	name := cmd.Name()
	if _, ok := view.subcommandParents[name]; ok {
		if len(args) > 1 {
			if !cscWireFaithfulText(args[1]) {
				return cscCommandMeta{}, false
			}
			sub := cmd.stringArg(1)
			if sub == "" {
				return cscCommandMeta{}, false
			}
			meta, ok := view.cscTable[name+"|"+internal.ToLower(sub)]
			return meta, ok
		}
	}
	meta, ok := view.cscTable[name]
	return meta, ok
}

// cscWireFaithfulText: policy tokens must read the same via stringArg as they
// go on the wire, or one command could be classified while another runs.
func cscWireFaithfulText(v interface{}) bool {
	switch v.(type) {
	case string, []byte, *string:
		return true
	default:
		return false
	}
}

// cscIsClientSideCacheable is the HLD's six-rule eligibility check.
func cscIsClientSideCacheable(meta cscCommandMeta) bool {
	return meta.bits&cscNegativeBits == 0 &&
		meta.bits&cscFlagReadonly != 0 &&
		cscHasKeyArgument(meta)
}

// lastKey is never consulted: it is -1 for variadic key lists.
func cscHasKeyArgument(meta cscCommandMeta) bool {
	return meta.bits&cscHasKeySpec != 0 || (meta.firstKey > 0 && meta.step > 0)
}

// cscEligibleMeta is the command-level decision: it resolves cmd's metadata
// in view and applies the eligibility check. Whether a specific call can be
// cached is decided later by extractRedisKeys.
func cscEligibleMeta(view *commandMetadataView, cmd Cmder) (cscCommandMeta, bool) {
	// Streaming replies (RawWriteToCmd) must not be buffered for the cache.
	if cmd.NoRetry() {
		return cscCommandMeta{}, false
	}
	meta, ok := cscLookupMeta(view, cmd)
	if !ok || !cscIsClientSideCacheable(meta) {
		return cscCommandMeta{}, false
	}
	return meta, true
}

// sortROHasByGet fails closed when an argument's wire text is unknown.
func sortROHasByGet(cmd Cmder) bool {
	args := cmd.Args()
	for i := 2; i < len(args); i++ {
		switch arg := args[i].(type) {
		case string:
			if strings.EqualFold(arg, "by") || strings.EqualFold(arg, "get") {
				return true
			}
		case []byte:
			if bytes.EqualFold(arg, []byte("by")) || bytes.EqualFold(arg, []byte("get")) {
				return true
			}
		case int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64,
			float32, float64, bool:
		default:
			return true
		}
	}
	return false
}

// cscCommandToken returns tokens with known wire representations without
// invoking BinaryMarshaler, which may be stateful.
func cscCommandToken(cmd Cmder, pos int) (string, bool) {
	args := cmd.Args()
	if pos < 0 || pos >= len(args) {
		return "", false
	}
	switch value := args[pos].(type) {
	case string:
		return value, true
	case *string:
		if value == nil {
			return "", true
		}
		return *value, true
	case []byte:
		return string(value), true
	default:
		return "", false
	}
}

func isSubscribeName(name string) bool {
	for i := range len(name) {
		if name[i] >= 0x80 {
			// Preserve ToLower's Unicode behavior, which differs from EqualFold.
			switch strings.ToLower(name) {
			case "subscribe", "psubscribe", "ssubscribe":
				return true
			default:
				return false
			}
		}
	}
	return strings.EqualFold(name, "subscribe") ||
		strings.EqualFold(name, "psubscribe") ||
		strings.EqualFold(name, "ssubscribe")
}

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

// buildCacheKeyNS renders the namespace parts and RESP arguments in one
// allocation (the returned string).
//
// Fusing the namespace in matters as much as pooling: the caller used to take
// this function's result and then do prefix+key, a second full copy of every
// cache key on every cached read. Writing the prefix into the buffer first
// makes the single String() produce the namespaced key directly, and the
// suffix after the namespace is still exactly the wire encoding, which the
// refresher relies on when it slices the namespace off to re-issue the command.
func buildCacheKeyNS(cmd Cmder, namespace ...string) (string, bool) {
	args := cmd.Args()
	if len(args) == 0 || !commandArgsRepeatable(cmd) {
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
	for _, part := range namespace {
		s.buf.WriteString(part)
	}
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

// cscRenderEntryKey includes the metadata fingerprint in the entry namespace.
func cscRenderEntryKey(keyPrefix, fingerprint string, cmd Cmder) (string, bool) {
	return buildCacheKeyNS(cmd, keyPrefix, fingerprint, cscNamespaceSep)
}

func cscEntryKeyPrefixLen(keyPrefix, fingerprint string) int {
	return len(keyPrefix) + len(fingerprint) + len(cscNamespaceSep)
}

// keyArg renders the arg at pos exactly as it goes on the wire; a mismatched
// key would never match its invalidation and be served stale forever, so
// types whose rendering can differ return ok=false (caller skips caching).
func keyArg(cmd Cmder, pos int) (string, bool) {
	if !keyArgOK(cmd, pos) {
		return "", false
	}
	return cmd.stringArg(pos), true
}

// keyArgOK validates a key without allocating its wire representation.
func keyArgOK(cmd Cmder, pos int) bool {
	args := cmd.Args()
	return pos >= 0 && pos < len(args) && isWireKeyType(args[pos])
}

// cscIntArg reads a count using the same types and integer range as
// keyArg followed by strconv.Atoi, without rendering numeric arguments.
func cscIntArg(cmd Cmder, pos int) (int, bool) {
	args := cmd.Args()
	if pos < 0 || pos >= len(args) {
		return 0, false
	}
	var text string
	switch value := args[pos].(type) {
	case string:
		text = value
	case []byte:
		text = string(value)
	case int:
		return value, true
	case int8:
		return int(value), true
	case int16:
		return int(value), true
	case int32:
		return int(value), true
	case int64:
		return int(value), value >= math.MinInt && value <= math.MaxInt
	case uint:
		return int(value), value <= uint(math.MaxInt)
	case uint8:
		return int(value), true
	case uint16:
		return int(value), true
	case uint32:
		return int(value), uint64(value) <= uint64(math.MaxInt)
	case uint64:
		return int(value), value <= uint64(math.MaxInt)
	default:
		return 0, false
	}
	value, err := strconv.Atoi(text)
	return value, err == nil
}

// cscCanExtractRedisKeys applies the same invocation checks without allocating
// the key slice that AutoPipeline's solo-dispatch gate does not need.
func cscCanExtractRedisKeys(meta cscCommandMeta, cmd Cmder) bool {
	first, step, count := cscRedisKeyLayout(meta, cmd)
	for i := range count {
		if !keyArgOK(cmd, first+i*step) {
			return false
		}
	}
	return count > 0
}

// cscRedisKeyLayout returns first, step, and count; count zero means uncached.
func cscRedisKeyLayout(meta cscCommandMeta, cmd Cmder) (int, int, int) {
	switch meta.guard {
	case cscInvocationGuardNone:
	case cscInvocationGuardSortRONoByGet:
		// BY/GET keys cannot be enumerated.
		if sortROHasByGet(cmd) {
			return 0, 0, 0
		}
	default:
		// Unknown guards cannot prove a complete key list.
		return 0, 0, 0
	}
	argsLen := len(cmd.Args())

	switch meta.extract {
	case cscKeyExtractNone:
		return 0, 0, 0

	case cscKeyExtractRange:
		first, step := int(meta.firstKey), int(meta.step)
		last := int(meta.lastKey)
		if last < 0 {
			last = argsLen + last
		}
		if first <= 0 || step <= 0 || last < first || last >= argsLen {
			return 0, 0, 0
		}
		return first, step, (last-first)/step + 1

	case cscKeyExtractKeynum:
		nkPos := int(meta.numkeysAt)
		if nkPos <= 0 || nkPos >= argsLen {
			return 0, 0, 0
		}
		// numkeys decides which positions are keys: read it wire-faithfully.
		numKeys, ok := cscIntArg(cmd, nkPos)
		if !ok || numKeys <= 0 {
			return 0, 0, 0
		}
		first, step := int(meta.firstKey), int(meta.step)
		if first <= 0 || first >= argsLen || step <= 0 {
			return 0, 0, 0
		}
		// The count must fit the args; division avoids overflow for any step.
		if numKeys > argsLen || (argsLen-1-first)/step < numKeys-1 {
			return 0, 0, 0
		}
		return first, step, numKeys
	}
	return 0, 0, 0
}
