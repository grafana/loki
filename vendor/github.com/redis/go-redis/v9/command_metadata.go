package redis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"math/rand"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9/internal"
)

// Per-client command-metadata resolution. A client resolves COMMAND metadata
// records into an immutable commandMetadataView (app overrides > built-in
// corrections > live COMMAND output > shipped snapshot) and publishes it
// through an atomic pointer. Every command invocation reads exactly one view.
// In PreferLive mode a background worker upgrades the static view with the
// server's live COMMAND output.

// CommandMetadataMode selects metadata resolution for CSC and Cluster routing.
//
// Experimental: this API may change in a minor release.
type CommandMetadataMode int

const (
	// CommandMetadataStatic uses the snapshot and overrides without network
	// calls or goroutines, unless a ClusterClient's Dynamic Resolver is used.
	CommandMetadataStatic CommandMetadataMode = iota

	// CommandMetadataPreferLive starts static and fetches live metadata after
	// the first connection. Older Redis versions and servers not identified as
	// Redis get conservative CSC exclusions.
	//
	// Security: live metadata controls routing and caching. Use TLS because a
	// compromised source can misroute commands, serve stale data, or suppress a
	// write by falsely classifying it as cacheable. Redis 8.10+ records are
	// authoritative except for built-in corrections.
	CommandMetadataPreferLive
)

// CommandMetadataConfig configures metadata for Options and ClusterOptions.
// Its zero value is static.
//
// Experimental: this API may change in a minor release.
type CommandMetadataConfig struct {
	Mode CommandMetadataMode

	// Overrides take precedence over every other metadata source. Keys are
	// case-insensitive; use "parent|child" for subcommands. Nil marks a command
	// unknown. Unsafe cacheable overrides can serve stale data. The map and
	// records are copied at client creation.
	Overrides map[string]*CommandInfo

	// RefreshInterval periodically refreshes live metadata with jitter.
	// Zero disables periodic refresh; detected server or topology changes
	// still trigger a refresh.
	RefreshInterval time.Duration
}

// commandMetadataView is one immutable resolution of command metadata.
type commandMetadataView struct {
	// generation identifies this view across clients and refreshes.
	generation uint64

	// records holds resolved metadata by lowercase command name.
	records map[string]*CommandInfo

	// tombstones block fallback for malformed or explicitly disabled commands.
	tombstones map[string]struct{}

	// shadowedParents are bare records pruned to expose their subcommands.
	subcommandParents map[string]struct{}
	shadowedParents   map[string]struct{}

	// CSC and routing derive separate decisions from the shared records.
	cscTable     map[string]cscCommandMeta
	routingTable map[string]*routingCommandMeta

	// cscFingerprint identifies the eligibility decisions. It is part of
	// every cache-entry key — never of the Redis-key invalidation index — so
	// entries cached under different metadata never mix, while invalidations
	// keep reaching every generation.
	cscFingerprint string

	// live is true when the view was built from live COMMAND output.
	live          bool
	serverVersion string
}

// defaultCommandMetadataView is built only when CSC or metadata routing needs
// it, and is shared by clients without overrides or live mode.
var defaultCommandMetadataView = sync.OnceValue(func() *commandMetadataView {
	return buildCommandMetadataView(nil, nil)
})

var commandMetadataGeneration atomic.Uint64

// The snapshot itself is static data. Only live compatibility resolution needs
// a name index; ordinary static view construction can iterate the records.
var commandInfoSnapshotByName = sync.OnceValue(func() map[string]*CommandInfo {
	index := make(map[string]*CommandInfo, len(commandInfoSnapshotRecords))
	for i := range commandInfoSnapshotRecords {
		info := &commandInfoSnapshotRecords[i]
		index[info.Name] = info
	}
	return index
})

// commandMetadataCompatibilityCorrection adds CSC exclusions missing before
// Redis 8.10.
type commandMetadataCompatibilityCorrection struct {
	flags []string
	tips  []string
}

// commandMetadataPre810Corrections contains only pre-8.10 CSC exclusions.
// Redis 8.10+ metadata stays authoritative except for built-in corrections.
var commandMetadataPre810Corrections = sync.OnceValue(func() map[string]commandMetadataCompatibilityCorrection {
	corrections := make(map[string]commandMetadataCompatibilityCorrection)
	for i := range commandInfoSnapshotRecords {
		info := &commandInfoSnapshotRecords[i]
		var correction commandMetadataCompatibilityCorrection
		if !commandRecordHas(info, "readonly", false) {
			// Do not let old metadata make a known write command cacheable.
			correction.tips = append(correction.tips, "dont_cache")
		}
		for _, flag := range info.Flags {
			switch flag {
			case "script_runner", "blocking":
				correction.flags = append(correction.flags, flag)
			}
		}
		for _, tip := range info.Tips {
			switch tip {
			case "dont_cache", "nondeterministic_output":
				correction.tips = append(correction.tips, tip)
			}
		}
		if len(correction.flags) > 0 || len(correction.tips) > 0 {
			corrections[info.Name] = correction
		}
	}
	return corrections
})

// buildCommandMetadataView builds a view, treating live input as Redis 8.10.
func buildCommandMetadataView(live, overrides map[string]*CommandInfo) *commandMetadataView {
	return buildCommandMetadataViewForServer(live, overrides, "redis", "8.10")
}

// buildCommandMetadataViewForServer resolves records and derived decisions.
func buildCommandMetadataViewForServer(
	live, overrides map[string]*CommandInfo,
	serverName, serverVersion string,
) *commandMetadataView {
	return buildCommandMetadataViewForServerWithLegacy(
		live,
		overrides,
		serverName,
		serverVersion,
		nil,
	)
}

func buildCommandMetadataViewForServerWithLegacy(
	live, overrides map[string]*CommandInfo,
	serverName, serverVersion string,
	liveLegacyRecords map[string]struct{},
) *commandMetadataView {
	records := make(map[string]*CommandInfo, len(commandInfoSnapshotRecords)+len(overrides))
	tombstones := make(map[string]struct{})
	parents := make(map[string]struct{})
	markParent := func(name string) {
		for offset := 0; ; {
			i := strings.IndexByte(name[offset:], '|')
			if i < 0 {
				break
			}
			offset += i
			if offset > 0 {
				parents[name[:offset]] = struct{}{}
			}
			offset++
		}
	}
	for i := range commandInfoSnapshotRecords {
		// Generated records are normalized and immutable. Mutable layers below
		// copy their inputs before applying changes.
		info := &commandInfoSnapshotRecords[i]
		records[info.Name] = info
		markParent(info.Name)
	}

	legacyLive := make(map[string]struct{}, len(liveLegacyRecords))
	for name := range liveLegacyRecords {
		legacyLive[internal.ToLower(name)] = struct{}{}
	}
	liveSupportsCSC := commandMetadataSupportsCSC(serverName, serverVersion)
	seenLive := make(map[string]struct{}, len(live))
	for name, info := range live {
		lower := internal.ToLower(name)
		// Tombstoned children still prevent their parent from shadowing them.
		markParent(lower)
		if _, duplicate := seenLive[lower]; duplicate {
			delete(records, lower)
			tombstones[lower] = struct{}{}
			continue
		}
		seenLive[lower] = struct{}{}
		if info == nil {
			// Live nil explicitly blocks snapshot fallback and corrections.
			delete(records, lower)
			tombstones[lower] = struct{}{}
			continue
		}
		_, legacy := legacyLive[lower]
		records[lower] = resolveLiveCommandMetadata(
			lower,
			info,
			liveSupportsCSC,
			legacy,
		)
	}
	// Apply built-in corrections before deriving CSC and routing decisions.
	for name, correction := range commandMetadataCorrections {
		markParent(name)
		if _, tombstoned := tombstones[name]; tombstoned {
			continue
		}
		rec := &CommandInfo{Name: name}
		if base := records[name]; base != nil {
			rec = cloneCommandInfo(base)
		}
		rec.Tips = appendCommandMetadataTokens(rec.Tips, correction.tips...)
		rec.Flags = removeCommandMetadataTokens(rec.Flags, correction.removeFlags...)
		if correction.keySpecs != nil {
			rec.KeySpecs = cloneCommandInfo(&CommandInfo{KeySpecs: correction.keySpecs}).KeySpecs
			rec.FirstKeyPos = correction.firstKeyPos
			rec.LastKeyPos = correction.lastKeyPos
			rec.StepCount = correction.stepCount
		}
		rec.ReadOnly = commandRecordHas(rec, "readonly", false)
		records[name] = rec
	}

	seenOverrides := make(map[string]struct{}, len(overrides))
	for name, info := range overrides {
		lower := internal.ToLower(name)
		markParent(lower)
		if _, duplicate := seenOverrides[lower]; duplicate {
			delete(records, lower)
			tombstones[lower] = struct{}{}
			continue
		}
		seenOverrides[lower] = struct{}{}
		if info == nil {
			// nil means "unknown": fail closed, don't expose lower layers.
			delete(records, lower)
			tombstones[lower] = struct{}{}
			continue
		}
		// Overrides replace all lower-priority records and corrections.
		delete(tombstones, lower)
		records[lower] = cloneCommandInfoForName(lower, info)
	}

	table := make(map[string]cscCommandMeta, len(records))
	for name, info := range records {
		table[name] = cscDeriveMeta(info)
	}
	// Keep only parents whose arity permits a bare invocation. Lookup still
	// checks subcommands first, so a parent cannot shadow its children.
	shadowedParents := make(map[string]struct{})
	for parent := range parents {
		if _, shadowed := table[parent]; shadowed && !commandMetadataAllowsBareInvocation(records[parent]) {
			delete(table, parent)
			shadowedParents[parent] = struct{}{}
		}
	}
	view := &commandMetadataView{
		generation:        commandMetadataGeneration.Add(1),
		records:           records,
		tombstones:        tombstones,
		subcommandParents: parents,
		shadowedParents:   shadowedParents,
		cscTable:          table,
		routingTable:      deriveRoutingTable(records, shadowedParents),
		cscFingerprint:    cscTableFingerprint(table),
		serverVersion:     serverVersion,
	}
	linkRoutingTemplates(view)
	return view
}

func commandMetadataAllowsBareInvocation(info *CommandInfo) bool {
	return info != nil && (info.Arity == 1 || info.Arity == -1)
}

// commandMetadataSupportsCSC reports whether live metadata has all CSC
// exclusion signals. Only Redis 8.10+ defines the complete set.
func commandMetadataSupportsCSC(serverName, version string) bool {
	if serverName != "redis" {
		return false
	}
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	return major > 8 || major == 8 && minor >= 10
}

// resolveLiveCommandMetadata applies server-specific restrictions to the
// shared record before consumers derive decisions.
func resolveLiveCommandMetadata(
	name string,
	info *CommandInfo,
	liveSupportsCSC bool,
	legacy bool,
) *CommandInfo {
	resolved := cloneCommandInfoForName(name, info)
	snapshot, snapshotKnown := commandInfoSnapshotByName()[name]
	if legacy && snapshotKnown {
		// Redis 5/6 COMMAND records cannot advertise routing policies. Keep
		// the known policies while retaining the server's flags and key positions.
		for _, tip := range snapshot.Tips {
			if strings.HasPrefix(tip, requestPolicy+":") || strings.HasPrefix(tip, responsePolicy+":") {
				resolved.Tips = appendCommandMetadataTokens(resolved.Tips, tip)
			}
		}
	}
	if !liveSupportsCSC {
		if correction, ok := commandMetadataPre810Corrections()[name]; ok {
			resolved.Flags = appendCommandMetadataTokens(resolved.Flags, correction.flags...)
			resolved.Tips = appendCommandMetadataTokens(resolved.Tips, correction.tips...)
		}
	}

	// Preserve legacy records for routing, but exclude unprovable records from
	// CSC. Snapshot-known pre-8.10 records keep their known exclusions above.
	if (!liveSupportsCSC && !snapshotKnown) || (liveSupportsCSC && legacy) {
		resolved.Tips = appendCommandMetadataTokens(resolved.Tips, "dont_cache")
	}
	return resolved
}

func cloneCommandInfoForName(name string, info *CommandInfo) *CommandInfo {
	cloned := cloneCommandInfo(info)
	cloned.Name = internal.ToLower(name)
	for i, flag := range cloned.Flags {
		cloned.Flags[i] = normalizeCommandMetadataFlag(flag)
	}
	for i, tip := range cloned.Tips {
		cloned.Tips[i] = normalizeCommandMetadataTip(tip)
	}
	for i := range cloned.KeySpecs {
		spec := &cloned.KeySpecs[i]
		spec.BeginSearch = normalizeCommandMetadataEnum(
			spec.BeginSearch,
			"index", "keyword", "unknown",
		)
		spec.FindKeys = normalizeCommandMetadataEnum(
			spec.FindKeys,
			"range", "keynum", "unknown",
		)
		for j, flag := range spec.Flags {
			spec.Flags[j] = normalizeCommandMetadataKeyFlag(flag)
		}
	}
	cloned.ReadOnly = commandRecordHas(cloned, "readonly", false)
	return cloned
}

func normalizeCommandMetadataFlag(flag string) string {
	key, _, hasValue := strings.Cut(flag, ":")
	lowerKey := internal.ToLower(key)
	switch lowerKey {
	case "blocking", "script_runner":
		// Preserve malformed negative signals conservatively.
		return lowerKey
	case "readonly":
		if !hasValue {
			return lowerKey
		}
		return flag
	default:
		return flag
	}
}

func normalizeCommandMetadataTip(tip string) string {
	key, value, hasValue := strings.Cut(tip, ":")
	lowerKey := internal.ToLower(key)
	switch lowerKey {
	case "dont_cache", "nondeterministic_output":
		// Preserve malformed negative signals conservatively.
		return lowerKey
	case requestPolicy:
		if hasValue {
			return lowerKey + ":" + normalizeCommandMetadataEnum(
				value,
				"", "default", "none", "all_nodes", "all_shards", "multi_shard", "special",
			)
		}
		return lowerKey
	case responsePolicy:
		if hasValue {
			return lowerKey + ":" + normalizeCommandMetadataEnum(
				value,
				"default(keyless)", "default(hashslot)", "all_succeeded", "one_succeeded",
				"agg_sum", "agg_min", "agg_max", "agg_logical_and", "agg_logical_or", "special",
			)
		}
		return lowerKey
	}
	return tip
}

func normalizeCommandMetadataEnum(value string, known ...string) string {
	lower := internal.ToLower(value)
	if slices.Contains(known, lower) {
		return lower
	}
	return value
}

func normalizeCommandMetadataKeyFlag(flag string) string {
	upper := strings.ToUpper(flag)
	switch upper {
	case "RO", "RW", "OW", "RM":
		return upper
	}
	lower := internal.ToLower(flag)
	switch lower {
	case "access", "update", "insert", "delete", "not_key", "incomplete", "variable_flags", "prefix":
		return lower
	default:
		return flag
	}
}

// commandMetadataKeySpecFlags is the validated key-spec meaning shared by CSC
// and Cluster routing. Unknown and prefix flags fail closed.
type commandMetadataKeySpecFlags struct {
	routingUsable bool
	cscComplete   bool
	planComplete  bool
}

func classifyCommandMetadataKeySpecFlags(flags []string) commandMetadataKeySpecFlags {
	seen := make(map[string]struct{}, len(flags))
	accessModes := 0
	actions := 0
	incomplete := false
	notKey := false
	for _, flag := range flags {
		if _, duplicate := seen[flag]; duplicate {
			return commandMetadataKeySpecFlags{}
		}
		seen[flag] = struct{}{}
		switch flag {
		case "RO", "RW", "OW", "RM":
			accessModes++
		case "access", "update", "insert", "delete":
			actions++
		case "variable_flags":
		case "incomplete":
			incomplete = true
		case "not_key":
			notKey = true
		case "prefix":
			return commandMetadataKeySpecFlags{}
		default:
			return commandMetadataKeySpecFlags{}
		}
	}

	// not_key arguments can route by slot but cannot prove CSC invalidation.
	if notKey {
		if accessModes != 0 || actions != 0 {
			return commandMetadataKeySpecFlags{}
		}
		return commandMetadataKeySpecFlags{
			routingUsable: true,
			planComplete:  !incomplete,
		}
	}

	// Ordinary key specs require exactly one access mode.
	if accessModes != 1 {
		return commandMetadataKeySpecFlags{}
	}
	return commandMetadataKeySpecFlags{
		routingUsable: true,
		cscComplete:   !incomplete,
		planComplete:  !incomplete,
	}
}

func appendCommandMetadataTokens(tokens []string, additions ...string) []string {
	for _, addition := range additions {
		if !slices.Contains(tokens, addition) {
			tokens = append(tokens, addition)
		}
	}
	return tokens
}

func removeCommandMetadataTokens(tokens []string, removals ...string) []string {
	if len(removals) == 0 {
		return tokens
	}
	return slices.DeleteFunc(tokens, func(token string) bool {
		return slices.Contains(removals, token)
	})
}

// cloneCommandInfo deep-copies a record so the copy can be mutated or
// published independently of the source.
func cloneCommandInfo(info *CommandInfo) *CommandInfo {
	cp := *info
	cp.Flags = append([]string(nil), info.Flags...)
	cp.ACLFlags = append([]string(nil), info.ACLFlags...)
	cp.Tips = append([]string(nil), info.Tips...)
	if info.KeySpecs != nil {
		cp.KeySpecs = make([]KeySpec, len(info.KeySpecs))
		for i, ks := range info.KeySpecs {
			cp.KeySpecs[i] = ks
			cp.KeySpecs[i].Flags = append([]string(nil), ks.Flags...)
		}
	}
	if info.CommandPolicy != nil {
		cp.CommandPolicy = cloneRoutingPolicy(info.CommandPolicy)
	}
	return &cp
}

// cscTableFingerprint hashes the derived eligibility table into a short
// stable token. Cryptographic (truncated SHA-256): in PreferLive mode the
// input is server-influenced, and a forged collision with an honest view's
// fingerprint would let a poisoned generation's entries survive a refresh.
func cscTableFingerprint(table map[string]cscCommandMeta) string {
	names := make([]string, 0, len(table))
	for name := range table {
		names = append(names, name)
	}
	sort.Strings(names)
	h := sha256.New()
	buf := make([]byte, 0, 128)
	for _, name := range names {
		m := table[name]
		buf = append(buf[:0], name...)
		buf = append(buf, 0)
		for _, value := range [...]int64{
			int64(m.bits), int64(m.extract), int64(m.guard),
			int64(m.firstKey), int64(m.lastKey), int64(m.step), int64(m.numkeysAt),
		} {
			buf = strconv.AppendInt(buf, value, 10)
			buf = append(buf, ' ')
		}
		buf[len(buf)-1] = '\n'
		h.Write(buf)
	}
	var digest [sha256.Size]byte
	return hex.EncodeToString(h.Sum(digest[:0])[:16])
}

// commandMetadataFetchResult separates publication identity from the server
// name and version used for compatibility rules.
type commandMetadataFetchResult struct {
	records           map[string]*CommandInfo
	legacyRecords     map[string]struct{}
	serverName        string
	serverVersion     string
	serverFingerprint string
}

func (m commandMetadataFetchResult) equal(other commandMetadataFetchResult) bool {
	return m.serverName == other.serverName &&
		m.serverVersion == other.serverVersion &&
		m.serverFingerprint == other.serverFingerprint &&
		maps.Equal(m.legacyRecords, other.legacyRecords) &&
		maps.EqualFunc(m.records, other.records, func(a, b *CommandInfo) bool {
			return reflect.DeepEqual(a, b)
		})
}

// A successful fetch transfers ownership of its records to the store.
type commandMetadataFetchFunc func(context.Context) (commandMetadataFetchResult, error)

// commandMetadataStore publishes immutable views for one client. Its pointer
// is shared with clones; the attaching client owns the worker.
type commandMetadataStore struct {
	current atomic.Pointer[commandMetadataView]
	// A dynamic resolver activates live tracking on its first lookup, including
	// for a statically configured store. Constructing an unused resolver does not.
	liveRequested atomic.Bool

	mode            CommandMetadataMode
	overrides       map[string]*CommandInfo
	refreshInterval time.Duration
	fetch           commandMetadataFetchFunc

	// static is the initial and fallback view.
	static *commandMetadataView

	// lastFetch is the source of the last successfully published live view.
	// Only the active refresh attempt reads or replaces it.
	lastFetch commandMetadataFetchResult

	// fetchCtx is cancelled by signalStop so Close never waits out an
	// in-flight fetch's own timeout.
	fetchCtx    context.Context
	fetchCancel context.CancelFunc

	mu          sync.Mutex
	refreshDone chan struct{} // guarded by mu; non-nil while one attempt owns lastFetch
	initialDone bool          // one completed attempt ends foreground verification
	retryAt     time.Time     // background cooldown survives new refresh requests
	backoff     time.Duration
	started     bool
	refresh     chan struct{}
	refreshLive atomic.Bool // A node identity observation also needs a live-view refresh.
	stop        chan struct{}
	done        chan struct{}
	stopOnce    sync.Once

	// serverFp identifies the current server. serverEpoch also rejects ABA
	// changes during a refresh. Both are guarded by mu.
	serverFp    string
	serverEpoch uint64
}

// newCommandMetadataStore returns nil for configs equivalent to the static
// default, letting those clients share defaultCommandMetadataView with zero
// allocations.
func newCommandMetadataStore(
	cfg *CommandMetadataConfig,
	fetch commandMetadataFetchFunc,
) *commandMetadataStore {
	return newCommandMetadataStoreWithLiveRequirement(cfg, fetch, false)
}

// newCommandMetadataStoreForLive creates a store even for static configuration.
func newCommandMetadataStoreForLive(
	cfg *CommandMetadataConfig,
	fetch commandMetadataFetchFunc,
) *commandMetadataStore {
	return newCommandMetadataStoreWithLiveRequirement(cfg, fetch, true)
}

func newCommandMetadataStoreWithLiveRequirement(
	cfg *CommandMetadataConfig,
	fetch commandMetadataFetchFunc,
	requireLive bool,
) *commandMetadataStore {
	if cfg == nil {
		cfg = &CommandMetadataConfig{}
	}
	if !requireLive && cfg.Mode == CommandMetadataStatic && len(cfg.Overrides) == 0 {
		return nil
	}
	// Deep-copy the overrides: refreshOnce re-reads them on every rebuild,
	// and an application mutating its config map would race the worker.
	var overrides map[string]*CommandInfo
	if len(cfg.Overrides) > 0 {
		overrides = make(map[string]*CommandInfo, len(cfg.Overrides))
		for name, info := range cfg.Overrides {
			name = internal.ToLower(name)
			if _, duplicate := overrides[name]; duplicate {
				// Tombstone ambiguous case-colliding overrides.
				overrides[name] = nil
				continue
			}
			if info == nil {
				overrides[name] = nil
				continue
			}
			overrides[name] = cloneCommandInfo(info)
		}
	}
	s := &commandMetadataStore{
		mode:            cfg.Mode,
		overrides:       overrides,
		refreshInterval: cfg.RefreshInterval,
		fetch:           fetch,
		refresh:         make(chan struct{}, 1),
		stop:            make(chan struct{}),
		done:            make(chan struct{}),
	}
	s.fetchCtx, s.fetchCancel = context.WithCancel(context.Background())
	if len(overrides) == 0 {
		s.static = defaultCommandMetadataView()
	} else {
		s.static = buildCommandMetadataView(nil, overrides)
	}
	s.current.Store(s.static)
	// An override keyed by a bare container name ("memory") is pruned in
	// favor of its subcommand entries and would otherwise vanish silently.
	for name, info := range overrides {
		if info == nil {
			continue
		}
		if _, shadowed := s.static.shadowedParents[name]; shadowed {
			internal.Logger.Printf(context.Background(),
				"redis: CommandMetadata override %q targets a container command and has no effect; key it \"parent|child\"", name)
		}
	}
	return s
}

func (s *commandMetadataStore) view() *commandMetadataView {
	return s.current.Load()
}

func (s *commandMetadataStore) wantsServerHello() bool {
	return s != nil && (s.mode == CommandMetadataPreferLive || s.liveRequested.Load())
}

// onConnInit runs after each successful connection initialization: in
// live mode it schedules the upgrade until one succeeds. Later drift without
// a new connection (module loads, upgrades) is covered by RefreshInterval.
func (s *commandMetadataStore) onConnInit() {
	if !s.wantsServerHello() {
		return
	}
	if v := s.current.Load(); v != nil && v.live {
		return
	}
	s.requestRefresh()
}

// ensureLive waits for one initial verification. Later calls use the conservative
// view immediately while background retries finish compatibility checks.
func (s *commandMetadataStore) ensureLive(ctx context.Context) error {
	if s == nil || s.fetch == nil {
		return fmt.Errorf("redis: live command metadata fetch is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !s.liveRequested.Load() && s.liveRequested.CompareAndSwap(false, true) && s.refreshInterval > 0 {
		// Periodic refresh is opt-in, including after an explicit dynamic lookup.
		s.startWorker()
	}
	if v := s.current.Load(); v != nil && v.live {
		return nil
	}
	err := s.refreshMetadata(ctx, commandMetadataInitialRefresh)
	if err != nil {
		s.requestRefresh()
	}
	return err
}

// onServerHello retires live metadata when the server identity changes.
func (s *commandMetadataStore) onServerHello(fp string) {
	if !s.wantsServerHello() {
		return
	}
	s.mu.Lock()
	changed := fp != s.serverFp
	if changed {
		s.serverFp = fp
		s.serverEpoch++
		if v := s.current.Load(); v != nil && v.live {
			// Decide on static metadata until the new server's arrives.
			s.current.Store(s.static)
		}
	}
	s.mu.Unlock()
	if !changed {
		s.onConnInit()
		return
	}
	s.requestRefresh()
}

// onClusterServerHelloReply keeps node callbacks dormant until live metadata
// is requested. A nil reply represents a connection initialized without HELLO.
func (s *commandMetadataStore) onClusterServerHelloReply(hello map[string]interface{}) {
	if !s.wantsServerHello() {
		return
	}
	fingerprint := ""
	if hello != nil {
		fingerprint = helloServerFingerprint(hello)
	}
	s.onClusterServerHello(fingerprint)
}

// onClusterServerHello schedules verification without adopting one node's
// identity. The cluster fetch checks siblings before retiring the live view.
// An empty fingerprint always requests verification, including during a fetch.
func (s *commandMetadataStore) onClusterServerHello(fp string) {
	if !s.wantsServerHello() {
		return
	}
	if fp != "" && fp == s.serverFingerprint() {
		s.onConnInit()
		return
	}
	s.requestVerification()
}

// requestVerification checks identity even when metadata is already live.
// HELLO-fallback reconnects cannot prove that the previous identity still holds.
func (s *commandMetadataStore) requestVerification() {
	if !s.wantsServerHello() {
		return
	}
	s.refreshLive.Store(true)
	s.requestRefresh()
}

func (s *commandMetadataStore) serverFingerprint() string {
	fp, _ := s.serverIdentity()
	return fp
}

func (s *commandMetadataStore) serverIdentity() (string, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.serverFp, s.serverEpoch
}

// publishLiveView atomically validates the server identity and publishes the
// fetched view. It adopts the fetched identity when none was known.
func (s *commandMetadataStore) publishLiveView(
	expectedFp string,
	expectedEpoch uint64,
	fetchedFp string,
	view *commandMetadataView,
) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.serverFp != expectedFp || s.serverEpoch != expectedEpoch {
		return false
	}
	// Never publish metadata without a verifiable source identity.
	if fetchedFp == "" || expectedFp != "" && expectedFp != fetchedFp {
		return false
	}
	if expectedFp == "" && fetchedFp != "" {
		s.serverFp = fetchedFp
	}
	s.current.Store(view)
	return true
}

// requestRefresh schedules one background refresh; pending requests coalesce.
func (s *commandMetadataStore) requestRefresh() {
	if !s.wantsServerHello() || s.fetch == nil {
		return
	}
	s.startWorker()
	select {
	case s.refresh <- struct{}{}:
	default:
	}
}

// invalidateLiveAndRequestRefresh retires old metadata and schedules a refresh.
func (s *commandMetadataStore) invalidateLiveAndRequestRefresh() {
	s.invalidateLive()
	s.requestRefresh()
}

// beginParentSourceChange blocks refresh publication across a topology change.
func (s *commandMetadataStore) beginParentSourceChange() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.invalidateLiveLocked()
}

func (s *commandMetadataStore) finishParentSourceChange() {
	if s == nil {
		return
	}
	s.mu.Unlock()
	s.requestRefresh()
}

// invalidateLive retires the live view without starting a fetch.
func (s *commandMetadataStore) invalidateLive() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.invalidateLiveLocked()
	s.mu.Unlock()
}

func (s *commandMetadataStore) invalidateLiveLocked() {
	s.serverFp = ""
	s.serverEpoch++
	if view := s.current.Load(); view != nil && view.live {
		s.current.Store(s.static)
	}
}

func (s *commandMetadataStore) startWorker() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return
	}
	select {
	case <-s.stop:
		return // already stopped; never start after Close
	default:
	}
	s.started = true
	go s.run()
}

// cmdMetaBackoffMin/Max bound the retry backoff after a failed refresh, and
// cmdMetaRetryCap bounds automatic retries (later external triggers —
// conn init, periodic refresh — still attempt once each). Vars so tests can
// shrink them.
var (
	cmdMetaBackoffMin = time.Second
	cmdMetaBackoffMax = 30 * time.Second
	cmdMetaRetryCap   = 5
)

const cmdMetaFetchTimeout = 30 * time.Second

var errCommandMetadataNotReady = errors.New("redis: live command metadata verification is pending")

type commandMetadataRefreshMode uint8

const (
	commandMetadataInitialRefresh commandMetadataRefreshMode = iota
	commandMetadataUpgrade
	commandMetadataVerify
)

func (s *commandMetadataStore) run() {
	defer close(s.done)
	var periodicC <-chan time.Time
	var periodic *time.Timer
	if s.refreshInterval > 0 {
		periodic = time.NewTimer(cmdMetaJitter(s.refreshInterval))
		defer periodic.Stop()
		periodicC = periodic.C
	}
	consecFail := 0
	attempt := func(verifyLive bool) {
		for {
			mode := commandMetadataUpgrade
			if verifyLive {
				mode = commandMetadataVerify
			}
			err := s.refreshMetadata(s.fetchCtx, mode)
			if err == nil {
				consecFail = 0
				return
			}
			consecFail++
			// Damp the log for persistent failures (e.g. COMMAND denied by ACL).
			if consecFail <= cmdMetaRetryCap || consecFail%10 == 0 {
				internal.Logger.Printf(context.Background(),
					"redis: command metadata refresh failed (attempt %d): %v", consecFail, err)
			}
			if consecFail >= cmdMetaRetryCap {
				// Stop self-retrying; the next external trigger tries again.
				return
			}
		}
	}
	for {
		select {
		case <-s.stop:
			return
		case <-periodicC:
			periodic.Reset(cmdMetaJitter(s.refreshInterval))
			attempt(true)
		case <-s.refresh:
			// Stop wins over a pending token, so Close never runs one more fetch.
			select {
			case <-s.stop:
				return
			default:
			}
			// Skip redundant connection-init requests, but verify identity
			// changes and HELLO-fallback reconnects even while the view is live.
			verifyLive := s.refreshLive.Swap(false)
			attempt(verifyLive)
		}
	}
}

// refreshMetadata serializes publication while allowing callers to cancel their
// own wait. After the first completed attempt only the worker retries failures.
func (s *commandMetadataStore) refreshMetadata(ctx context.Context, mode commandMetadataRefreshMode) (err error) {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.fetchCtx.Err(); err != nil {
			return err
		}
		s.mu.Lock()
		if v := s.current.Load(); mode != commandMetadataVerify && v != nil && v.live {
			s.mu.Unlock()
			return nil
		}
		if mode == commandMetadataInitialRefresh && s.initialDone {
			s.mu.Unlock()
			return errCommandMetadataNotReady
		}
		if done := s.refreshDone; done != nil {
			s.mu.Unlock()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-s.stop:
				return context.Canceled
			case <-done:
				// Initial callers share the outcome without another foreground
				// probe; forced verification still performs a fresh attempt.
				continue
			}
		}
		if delay := time.Until(s.retryAt); mode != commandMetadataInitialRefresh && delay > 0 {
			s.mu.Unlock()
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-s.stop:
				timer.Stop()
				return context.Canceled
			case <-timer.C:
				continue
			}
		}
		done := make(chan struct{})
		s.refreshDone = done
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			s.initialDone = true
			if err == nil {
				s.backoff, s.retryAt = 0, time.Time{}
			} else if ctx.Err() == nil {
				if s.backoff == 0 {
					s.backoff = cmdMetaBackoffMin
				} else {
					s.backoff = min(s.backoff*2, cmdMetaBackoffMax)
				}
				s.retryAt = time.Now().Add(s.backoff)
			}
			s.refreshDone = nil
			close(done)
			s.mu.Unlock()
		}()
		return s.fetchAndPublish(ctx)
	}
}

// fetchAndPublish runs only for the caller owning refreshDone.
func (s *commandMetadataStore) fetchAndPublish(parent context.Context) (err error) {
	fpStart, epochStart := s.serverIdentity()
	ctx, cancel := context.WithTimeout(parent, cmdMetaFetchTimeout)
	stopFetchCancel := context.AfterFunc(s.fetchCtx, cancel)
	defer stopFetchCancel()
	defer cancel()

	// Isolate the fetch so stop/join also stays bounded if application hooks
	// or a custom fetch callback ignore cancellation. Built-in network probes
	// honor the bounded context; an abandoned callback must eventually return.
	type fetchResult struct {
		metadata commandMetadataFetchResult
		err      error
	}
	ch := make(chan fetchResult, 1)
	go func() {
		// A hostile or corrupt COMMAND reply must fail the refresh, not kill
		// the process from a goroutine the application cannot wrap.
		defer func() {
			if r := recover(); r != nil {
				ch <- fetchResult{err: fmt.Errorf("panic during command metadata refresh: %v", r)}
			}
		}()
		metadata, err := s.fetch(ctx)
		ch <- fetchResult{metadata: metadata, err: err}
	}()
	var metadata commandMetadataFetchResult
	select {
	case <-ctx.Done():
		return ctx.Err()
	case res := <-ch:
		if res.err != nil {
			return res.err
		}
		metadata = res.metadata
	}
	if fp, epoch := s.serverIdentity(); fp != fpStart || epoch != epochStart {
		// Do not publish across a server change.
		return fmt.Errorf("command metadata server changed during refresh")
	}
	view := s.view()
	if !view.live || !metadata.equal(s.lastFetch) {
		view = buildCommandMetadataViewForServerWithLegacy(
			metadata.records,
			s.overrides,
			metadata.serverName,
			metadata.serverVersion,
			metadata.legacyRecords,
		)
		view.live = true
	}
	// Reusing a view must still validate the identity and epoch under the
	// publication lock: either may have changed during the comparison.
	if !s.publishLiveView(fpStart, epochStart, metadata.serverFingerprint, view) {
		return fmt.Errorf("command metadata server changed during publication")
	}
	s.lastFetch = metadata
	return nil
}

func commandRecordHas(info *CommandInfo, token string, inTips bool) bool {
	if info == nil {
		return false
	}
	list := info.Flags
	if inTips {
		list = info.Tips
	}
	return slices.Contains(list, token)
}

// cmdMetaJitter spreads periodic refreshes by +-10% to avoid synchronized
// fetches across a fleet.
func cmdMetaJitter(d time.Duration) time.Duration {
	return d + time.Duration(rand.Int63n(int64(d)/5+1)) - d/10
}

// signalStop makes the worker exit without joining; safe from a GC cleanup.
// It also cancels any in-flight fetch so a join never waits out the fetch
// timeout.
func (s *commandMetadataStore) signalStop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stop)
		s.fetchCancel()
	})
}

// stopAndJoin stops the worker and waits for it to exit. Called by the owning
// client's Close before the pools the fetch path uses are torn down; clones
// never call it.
func (s *commandMetadataStore) stopAndJoin() {
	if s == nil {
		return
	}
	s.signalStop()
	s.mu.Lock()
	started := s.started
	s.mu.Unlock()
	if started {
		<-s.done
	}
}

// helloServerFingerprint condenses a HELLO reply into a server-identity
// token (server name, version and sorted module name:ver pairs) for change detection.
func helloServerFingerprint(reply map[string]interface{}) string {
	version := helloServerVersion(reply)
	if version == "" {
		return ""
	}
	var b strings.Builder
	if name := helloServerName(reply); name != "" {
		b.WriteString(name)
		b.WriteByte(':')
	}
	b.WriteString(version)
	if mods, ok := reply["modules"].([]interface{}); ok {
		names := make([]string, 0, len(mods))
		for _, m := range mods {
			switch mm := m.(type) {
			case map[interface{}]interface{}:
				names = append(names, fmt.Sprintf("%v:%v", mm["name"], mm["ver"]))
			case map[string]interface{}:
				names = append(names, fmt.Sprintf("%v:%v", mm["name"], mm["ver"]))
			case []interface{}:
				// HELLO 2 represents module maps as alternating key/value pairs.
				var name, version interface{}
				for i := 0; i+1 < len(mm); i += 2 {
					switch mm[i] {
					case "name":
						name = mm[i+1]
					case "ver":
						version = mm[i+1]
					}
				}
				names = append(names, fmt.Sprintf("%v:%v", name, version))
			}
		}
		sort.Strings(names)
		for _, n := range names {
			b.WriteString("|")
			b.WriteString(n)
		}
	}
	return b.String()
}

func helloServerName(reply map[string]interface{}) string {
	name, _ := reply["server"].(string)
	return name
}

func helloServerVersion(reply map[string]interface{}) string {
	version, _ := reply["version"].(string)
	return version
}

// shareCommandMetadata equips an uncached derived client (Conn, Tx, initConn
// handshake) with read-only access to the parent's command-metadata store so
// custom CommandMetadata overrides still drive cscTrackable / OPTOUT prefixes.
func (c *baseClient) shareCommandMetadata(dst *baseClient) {
	if c.cmdMeta != nil {
		dst.metadataSource = c.cmdMeta
	} else if c.metadataSource != nil {
		dst.metadataSource = c.metadataSource
	}
	dst.staticCmdMeta = c.staticCmdMeta
}

// metadataView returns the client's current command-metadata view; clients
// without a store share the static default.
func (c *baseClient) metadataView() *commandMetadataView {
	if source := c.metadataSource; source != nil {
		return source.view()
	}
	if s := c.cmdMeta; s != nil {
		return s.view()
	}
	if c.staticCmdMeta != nil {
		return c.staticCmdMeta
	}
	return defaultCommandMetadataView()
}

// fetchCommandMetadata retrieves HELLO and COMMAND on one connection. It
// bypasses process hooks and CSC, but retains connection initialization hooks.
func (c *baseClient) fetchCommandMetadata(ctx context.Context) (commandMetadataFetchResult, error) {
	infoCmd := NewCommandsInfoCmd(ctx, "command")
	metadata, err := c.fetchMetadataIdentity(ctx, infoCmd)
	if err != nil {
		return commandMetadataFetchResult{}, err
	}
	metadata.records = infoCmd.valWithTombstones()
	err = infoCmd.Err()
	metadata.legacyRecords = infoCmd.legacyRecords
	return metadata, err
}

// fetchMetadataIdentity optionally includes COMMAND in the same pipeline, so
// records and identity describe one connection. A nil infoCmd only reads identity.
func (c *baseClient) fetchMetadataIdentity(ctx context.Context, infoCmd *CommandsInfoCmd) (commandMetadataFetchResult, error) {
	// One silent node must not consume the full cluster refresh budget. Keep
	// each attempt within the configured read timeout; disabled socket timeouts
	// still get the normal five-second default for this internal probe.
	timeout := c.opt.ReadTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Metadata refresh owns a bounded internal context independently of the
	// application's ContextTimeoutEnabled and socket-timeout choices. Run the
	// command through a lightweight clone whose zero socket timeouts defer to
	// that context deadline; otherwise ReadTimeout=-1 would leave a stalled
	// COMMAND holding a pooled connection after refreshOnce stopped waiting.
	fetchClient := c.withTimeout(0)
	// TODO: make baseClient.withTimeout preserve its hook snapshot.
	fetchClient.hooksMixin = c.hooksMixin.clone()
	fetchClient.opt.ContextTimeoutEnabled = true

	// Send HELLO and COMMAND in one pipeline. Each pipeline attempt holds one
	// physical connection for its entire write/read cycle, which proves which
	// server supplied the COMMAND reply even while a maintenance handoff leaves
	// connections to both endpoints in the pool. Do not wrap the pool in a
	// StickyConnPool: claiming a tracked connection there revokes its parent CSC
	// coverage and unnecessarily evicts that connection's cache entries on every
	// refresh.
	expectedFp := ""
	if c.cmdMeta != nil {
		expectedFp = c.cmdMeta.serverFingerprint()
	}
	fetchClient.pipelinePool = nil
	// Initialization of the borrowed connection must not change the target
	// identity while this fetch is validating it; the parent connection init
	// already owns onServerHello notifications.
	fetchClient.cmdMeta = nil
	fetchClient.metadataSource = nil
	fetchClient.opt.onServerHello = nil

	helloCmd := NewMapStringInterfaceCmd(ctx, "hello")
	cmds := []Cmder{helloCmd}
	if infoCmd != nil {
		cmds = append(cmds, infoCmd)
	}
	err := fetchClient.processPipeline(ctx, cmds)
	hello := helloCmd.Val()
	if err != nil {
		// Match initConn's RESP2 fallback for servers that reject HELLO.
		// Transport, parsing, and COMMAND errors still fail the refresh.
		if !isRedisError(helloCmd.Err()) || infoCmd != nil && infoCmd.Err() != nil {
			return commandMetadataFetchResult{}, err
		}
		serverCmd := NewInfoCmd(ctx, "info", "server")
		modulesCmd := NewMapStringInterfaceSliceCmd(ctx, "module", "list")
		// The pool may lend a different connection now. Fetch COMMAND again
		// alongside its identity rather than pairing replies across connections.
		cmds = []Cmder{serverCmd, modulesCmd}
		if infoCmd != nil {
			cmds = append(cmds, infoCmd)
		}
		if err := fetchClient.processPipeline(ctx, cmds); err != nil {
			return commandMetadataFetchResult{}, err
		}
		modules := make([]interface{}, len(modulesCmd.Val()))
		for i, module := range modulesCmd.Val() {
			name, nameOK := module["name"].(string)
			_, versionOK := module["ver"].(int64)
			if !nameOK || name == "" || !versionOK {
				return commandMetadataFetchResult{}, fmt.Errorf("command metadata MODULE LIST reply has no module name or version")
			}
			modules[i] = module
		}
		hello = map[string]interface{}{
			// INFO's redis_version does not identify the server as Redis.
			// Leave the server name unknown so CSC uses conservative rules.
			"version": serverCmd.Item("Server", "redis_version"),
			"modules": modules,
		}
	}
	serverVersion := helloServerVersion(hello)
	if serverVersion == "" {
		return commandMetadataFetchResult{}, fmt.Errorf("command metadata reply has no server version")
	}
	actualFp := helloServerFingerprint(hello)
	if expectedFp != "" && actualFp != expectedFp {
		// Notify the owner when this fetch first observes a server change.
		if c.cmdMeta != nil {
			c.cmdMeta.onServerHello(actualFp)
		}
		return commandMetadataFetchResult{}, fmt.Errorf(
			"command metadata server changed: got %q, want %q", actualFp, expectedFp,
		)
	}
	return commandMetadataFetchResult{
		serverName:        helloServerName(hello),
		serverVersion:     serverVersion,
		serverFingerprint: actualFp,
	}, nil
}
