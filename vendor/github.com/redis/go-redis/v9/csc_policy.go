package redis

import (
	"context"
	"fmt"
	"strings"
)

// TrackingMode selects the server-assisted cache tracking mode. It is fixed
// when a client is constructed and applied during each connection handshake.
type TrackingMode uint8

const (
	TrackingModePlain TrackingMode = iota
	TrackingModeOptIn
	TrackingModeOptOut
)

// CacheablePredicate selects among eligible, unmarked calls in every mode.
// command is the canonical metadata identifier; keys are actual Redis keys.
// The callback runs outside cache locks and may run concurrently. It must be
// fast, must not mutate keys, and must not call client control methods. The
// slice is owned by this invocation and may be retained by the callback.
type CacheablePredicate func(command string, keys []string) bool

type cscPolicy struct {
	mode      TrackingMode
	cacheable CacheablePredicate
}

// String returns the Redis tracking-mode name.
func (m TrackingMode) String() string {
	switch m {
	case TrackingModePlain:
		return "plain"
	case TrackingModeOptIn:
		return "optin"
	case TrackingModeOptOut:
		return "optout"
	default:
		return fmt.Sprintf("TrackingMode(%d)", uint8(m))
	}
}

func (opt *Options) cachePolicy() cscPolicy {
	return cscPolicy{opt.ClientSideCacheTrackingMode, opt.ClientSideCacheable}
}

func (opt *ClusterOptions) cachePolicy() cscPolicy {
	return cscPolicy{opt.ClientSideCacheTrackingMode, opt.ClientSideCacheable}
}

func (p cscPolicy) configured() bool {
	return p.mode != TrackingModePlain || p.cacheable != nil
}

type cacheMark uint8

const (
	cacheUnmarked cacheMark = iota
	cacheRequested
	cacheBypassed
)

// After capturing the context mark, execution state is carried by the command.
// Like command arguments/results it is owned by one execution at a time.
type cscIntent uint8

const (
	cscIntentUnresolved cscIntent = iota
	cscIntentCache
	cscIntentBypass
)

// Keep small state separate from the pinned metadata pointer so baseCmd can
// pack it alongside its other small fields.
type cscCommandState struct {
	flags uint8
	depth uint8
}

const (
	cscMarkMask    uint8 = 3
	cscIntentMask  uint8 = 3 << 2
	cscForceBypass uint8 = 1 << 4
	cscNoLookup    uint8 = 1 << 5
	cscAsk         uint8 = 1 << 6
)

func (s *cscCommandState) mark() cacheMark { return cacheMark(s.flags & cscMarkMask) }
func (s *cscCommandState) setMark(mark cacheMark) {
	s.flags = s.flags&^cscMarkMask | uint8(mark)
}
func (s *cscCommandState) intent() cscIntent { return cscIntent((s.flags & cscIntentMask) >> 2) }
func (s *cscCommandState) setIntent(intent cscIntent) {
	s.flags = s.flags&^cscIntentMask | uint8(intent)<<2
}
func (s *cscCommandState) has(flag uint8) bool { return s.flags&flag != 0 }
func (s *cscCommandState) set(flag uint8, value bool) {
	if value {
		s.flags |= flag
	} else {
		s.flags &^= flag
	}
}

func resetCSCCommand(cmd Cmder) {
	*cmd.cacheState() = cscCommandState{}
	cmd.setCacheView(nil)
}

func cscEnter(cmd Cmder) { cmd.cacheState().depth++ }

func cscLeave(cmd Cmder) {
	s := cmd.cacheState()
	s.depth--
	if s.depth == 0 {
		resetCSCCommand(cmd)
	}
}

type cacheContextKey struct{}

// WithCache returns a context that requests caching for Redis commands using it.
// The mark overrides the cache predicate and tracking-mode default, but cannot
// make an ineligible command cacheable. If caching is unavailable for the command
// or execution path, it executes normally without caching or a warning/error due
// to the mark. WithCache does not guarantee that a reply is stored.
// Deadlines, cancellation and other values are inherited from ctx. The context
// may be reused; each submitted command gets the mark. The most recent mark wins
// when WithCache and WithoutCache are nested, without changing their parent.
func WithCache(ctx context.Context) context.Context {
	return context.WithValue(ctx, cacheContextKey{}, cacheRequested)
}

// WithoutCache returns a context that bypasses caching for Redis commands using
// it. Reads go to the server without using or changing existing cache entries.
// The most recently applied cache mark wins; ctx itself is unchanged.
func WithoutCache(ctx context.Context) context.Context {
	return context.WithValue(ctx, cacheContextKey{}, cacheBypassed)
}

// Internal operations inherit cancellation and values, but not cache intent.
// Preserve the original context without allocating when it is already unmarked.
func cscUnmarkedContext(ctx context.Context) context.Context {
	if mark, _ := ctx.Value(cacheContextKey{}).(cacheMark); mark != cacheUnmarked {
		return context.WithValue(ctx, cacheContextKey{}, cacheUnmarked)
	}
	return ctx
}

// Capture intent before hooks or asynchronous dispatch can replace the context.
// Internal operation boundaries mask the context; children and retries keep
// the captured command state when hooks or batching replace their context.
func cscBegin(ctx context.Context, cmd Cmder) bool {
	s := cmd.cacheState()
	mark, _ := ctx.Value(cacheContextKey{}).(cacheMark)
	if s.depth == 0 {
		// Every new invocation overrides decisions copied by Clone, including
		// an unmarked call that selects the predicate or tracking-mode default.
		// Internal children/retries keep their active invocation.
		resetCSCCommand(cmd)
		s.setMark(mark)
	}
	if s.depth != 0 || s.mark() != cacheUnmarked || s.has(cscForceBypass) {
		cscEnter(cmd)
		return true
	}
	return false
}

// cscAdmitMarked fixes capability at submission, not at batch-size-dependent
// dispatch. Ordered async callers must not overtake earlier queued commands.
func (ap *AutoPipeliner) cscAdmitMarked(cmd Cmder) bool {
	if ap.blocking || ap.config.Unordered {
		return true
	}
	s := cmd.cacheState()
	s.set(cscForceBypass, true)
	s.setIntent(cscIntentBypass)
	return false
}

func (c *baseClient) cscDecide(ctx context.Context, cmd Cmder, view *commandMetadataView) (cscCommandMeta, bool) {
	s := cmd.cacheState()
	meta, eligible := cscInvocationMeta(view, cmd)
	capable := eligible && c.csc != nil && c.autopipelineCSCActive() && !s.has(cscForceBypass) && !s.has(cscAsk) && !cscIsBypassed(ctx)
	if c.opt.cscPresenceOnly && clusterCSCCommandPolicy(cmd.Name()) == clusterCSCReplyBypass {
		capable = false
	}
	if !capable {
		return meta, false
	}
	cscResolveIntent(cmd, view, meta, c.cscPolicy)
	return meta, s.intent() == cscIntentCache
}

func cscInvocationMeta(view *commandMetadataView, cmd Cmder) (cscCommandMeta, bool) {
	meta, eligible := cscEligibleMeta(view, cmd)
	if !eligible || !commandArgsRepeatable(cmd) {
		return meta, false
	}
	first, step, count := cscRedisKeyLayout(meta, cmd)
	for i, pos := 0, first; i < count; i, pos = i+1, pos+step {
		if !keyArgOK(cmd, pos) {
			return meta, false
		}
	}
	return meta, count > 0
}

func cscResolveIntent(cmd Cmder, view *commandMetadataView, meta cscCommandMeta, policy cscPolicy) {
	s := cmd.cacheState()
	if s.intent() == cscIntentUnresolved {
		wantCache := s.mark() == cacheRequested || (s.mark() == cacheUnmarked && policy.mode != TrackingModeOptIn)
		if s.mark() == cacheUnmarked && policy.cacheable != nil {
			first, step, count := cscRedisKeyLayout(meta, cmd)
			keys := make([]string, count)
			for i, pos := 0, first; i < count; i, pos = i+1, pos+step {
				keys[i], _ = keyArg(cmd, pos)
			}
			name := cmd.Name()
			if sub, ok := cscCommandToken(cmd, 1); ok {
				if _, ok := view.cscTable[name+"|"+strings.ToLower(sub)]; ok {
					name += "|" + strings.ToLower(sub)
				}
			}
			wantCache = policy.cacheable(name, keys)
		}
		s.setIntent(cscIntentBypass)
		if wantCache {
			s.setIntent(cscIntentCache)
		}
	}
}

func (c *baseClient) cscInvocationView(cmd Cmder) *commandMetadataView {
	if view := cmd.cacheView(); view != nil {
		return view
	}
	return c.metadataView()
}

// Resolve application intent on the complete original call, before fragments
// change its arguments. Each target still validates eligibility and coverage.
func (c *ClusterClient) cscPrepareInvocation(cmd Cmder) {
	s := cmd.cacheState()
	meta, eligible := cscInvocationMeta(cmd.cacheView(), cmd)
	capable := eligible && c.opt.Protocol == 3 &&
		(c.opt.ClientSideCacheConfig != nil || c.opt.cscCustomFactory) &&
		c.opt.StreamingCredentialsProvider == nil && c.opt.CredentialsProvider == nil && c.opt.CredentialsProviderContext == nil &&
		clusterCSCCommandPolicy(cmd.Name()) != clusterCSCReplyBypass
	if !capable {
		s.set(cscForceBypass, true)
		return
	}
	cscResolveIntent(cmd, cmd.cacheView(), meta, c.opt.cachePolicy())
}

func (c *baseClient) cscTrackable(cmd Cmder) bool {
	meta, ok := cscLookupMeta(c.cscInvocationView(cmd), cmd)
	if !ok || meta.bits&cscFlagReadonly == 0 {
		return false
	}
	first, step, count := cscRedisKeyLayout(meta, cmd)
	if count == 0 {
		return false
	}
	for i, pos := 0, first; i < count; i, pos = i+1, pos+step {
		if keyArgOK(cmd, pos) {
			continue
		}
		// Tracking needs a known wire key, not the narrower cache-key contract.
		// This conversion never invokes a user BinaryMarshaler.
		if _, ok := routingArgText(cmd, pos); !ok {
			return false
		}
	}
	return true
}

// A custom factory must honor the requested policy. Fail closed for observable
// omissions instead of storing through an unexpectedly configured node cache.
func (c *ClusterClient) cscCheckNode(cmd Cmder, node *Client) {
	if cmd.cacheState().has(cscAsk) {
		return
	}
	p := c.opt.cachePolicy()
	s := cmd.cacheState()
	if !p.configured() && !c.opt.cscCustomFactory {
		return
	}

	if p.mode == node.cscPolicy.mode && (p.cacheable == nil) == (node.cscPolicy.cacheable == nil) && node.metadataSource == c.cmdMeta {
		return
	}
	s.set(cscForceBypass, true)
}

func (hs *hooksMixin) cscCapturesIntent() bool { return hs.captureCacheIntent }
