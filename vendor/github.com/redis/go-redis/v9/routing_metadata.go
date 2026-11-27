package redis

import (
	"errors"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9/internal"
	"github.com/redis/go-redis/v9/internal/routing"
)

// commandArgsRepeatable accepts the types proto.Writer encodes directly.
// Unsupported values and marshalers must not reach a partial fan-out write.
func commandArgsRepeatable(cmd Cmder) bool {
	for _, arg := range cmd.Args() {
		switch arg.(type) {
		case nil, string, *string, []byte,
			int, *int, int8, *int8, int16, *int16, int32, *int32, int64, *int64,
			uint, *uint, uint8, *uint8, uint16, *uint16, uint32, *uint32, uint64, *uint64,
			uintptr, *uintptr, float32, *float32, float64, *float64, bool, *bool,
			time.Time, *time.Time, time.Duration, *time.Duration, net.IP:
			// proto.Writer encodes these directly.
		default:
			return false
		}
	}
	return true
}

var errUnsupportedRoutingPolicy = errors.New("redis: command requires an unsupported special routing policy")

type routingKeyState uint8

const (
	routingKeysUnknown routingKeyState = iota
	routingKeysNone
	routingKeysKnown
)

// routingMetadataState distinguishes missing records from unusable ones.
type routingMetadataState uint8

const (
	routingMetadataMissing routingMetadataState = iota
	routingMetadataUsable
	routingMetadataUnusable
)

type routingBeginSearch uint8

const (
	routingBeginIndex routingBeginSearch = iota
	routingBeginKeyword
	routingBeginStreams
	routingBeginMigrate
)

type routingFindKeys uint8

const (
	routingFindRange routingFindKeys = iota
	routingFindKeynum
)

// routingKeySpec is the validated routing form of one COMMAND key spec.
type routingKeySpec struct {
	begin routingBeginSearch
	find  routingFindKeys

	index     int
	keyword   string
	startFrom int

	lastKey   int
	step      int
	limit     int
	keyNumIdx int
	firstKey  int
}

type routingSpecialSupport uint8

const (
	routingSpecialRequestSupported routingSpecialSupport = 1 << iota
	routingSpecialResponseSupported
	routingSpecialRequestDeclared
	routingSpecialResponseDeclared
)

// routingCommandMeta ignores CommandInfo's compatibility CommandPolicy field.
type routingCommandMeta struct {
	name     string
	policy   *routing.CommandPolicy
	keySpecs []routingKeySpec
	// template holds the ordinary keyed decision shared by this immutable view.
	// Its invocation-specific slot is stored on the admitted command.
	template *clusterRoutingDecision
	// defaultTemplate is set (atomically, 1) once the default resolver produced
	// exactly template for this command; the fast path then uses it directly.
	defaultTemplate uint32
	readOnly        bool
	keyState        routingKeyState
	keyPlanComplete bool
	special         routingSpecialSupport
	valid           bool
}

// linkRoutingTemplates prepares allocation-free pins for fixed-index keyed
// commands. Admission still resolves normally and checks the entire decision
// before sharing a template; custom policies and malformed arguments fall back.
func linkRoutingTemplates(view *commandMetadataView) {
	for _, meta := range view.routingTable {
		if meta.keyState != routingKeysKnown || len(meta.keySpecs) != 1 {
			continue
		}
		spec := meta.keySpecs[0]
		if spec.begin != routingBeginIndex || spec.find != routingFindRange || spec.index <= 0 {
			continue
		}
		meta.template = &clusterRoutingDecision{
			view: view, meta: meta, firstKey: spec.index, naturalSlot: -1,
			metadataState: routingMetadataUsable, metaOK: true,
			readOnly: meta.policy != nil && meta.readOnly, template: true,
		}
	}
}

// routingKeyPlan contains all keys and split fields for one invocation.
type routingKeyPlan struct {
	positions  []int
	keyArgsEnd int
	step       int
	numKeysPos int
	splittable bool
}

// routingSpecialPolicies lists the implemented command-specific handlers.
var routingSpecialPolicies = map[string]routingSpecialSupport{
	"ft.cursor|del":  routingSpecialRequestDeclared | routingSpecialRequestSupported,
	"ft.cursor|read": routingSpecialRequestDeclared | routingSpecialRequestSupported,
	"randomkey":      routingSpecialResponseDeclared | routingSpecialResponseSupported,
	// SCAN walks every master (or the master of a hash-tag MATCH); see executeScanCommand.
	"scan": routingSpecialRequestDeclared | routingSpecialRequestSupported |
		routingSpecialResponseDeclared | routingSpecialResponseSupported,
}

// These keyless commands retain their node-local replies and cursors. Their
// special metadata does not define an implemented cluster-wide aggregation.
func routingLocalSpecialCommand(name string) bool {
	switch name {
	case "info", "bless|scan", "function|stats",
		"hotkeys|get", "hotkeys|reset", "hotkeys|start", "hotkeys|stop",
		"latency|doctor", "latency|graph", "latency|histogram", "latency|history", "latency|latest",
		"memory|doctor", "memory|malloc-stats", "memory|stats":
		return true
	default:
		return false
	}
}

// deriveRoutingTable omits tombstones and shadowed containers.
func deriveRoutingTable(records map[string]*CommandInfo, shadowedParents map[string]struct{}) map[string]*routingCommandMeta {
	table := make(map[string]*routingCommandMeta, len(records))
	for rawName, info := range records {
		name := internal.ToLower(rawName)
		if info == nil {
			continue
		}
		if _, shadowed := shadowedParents[name]; shadowed {
			continue
		}
		meta := deriveRoutingCommandMeta(name, info)
		if meta.valid {
			table[name] = meta
		}
	}
	return table
}

func deriveRoutingCommandMeta(name string, info *CommandInfo) *routingCommandMeta {
	meta := &routingCommandMeta{
		name: name, valid: info != nil,
		special: routingSpecialPolicies[name],
	}
	if info == nil {
		return meta
	}

	req, resp := routing.ReqDefault, routing.RespDefaultKeyless
	reqSet, respSet := false, false
	tips := make(map[string]string, len(info.Tips)+1)
	for _, raw := range info.Tips {
		key, value, hasValue := strings.Cut(raw, ":")
		switch key {
		case requestPolicy:
			if !hasValue || reqSet {
				meta.valid = false
				return meta
			}
			parsed, err := routing.ParseRequestPolicy(value)
			if err != nil {
				meta.valid = false
				return meta
			}
			req, reqSet = parsed, true
		case responsePolicy:
			if !hasValue || respSet {
				meta.valid = false
				return meta
			}
			parsed, err := routing.ParseResponsePolicy(value)
			if err != nil {
				meta.valid = false
				return meta
			}
			resp, respSet = parsed, true
		default:
			tips[key] = value
		}
	}
	for _, flag := range info.Flags {
		if flag == routing.ReadOnlyCMD {
			meta.readOnly = true
			tips[routing.ReadOnlyCMD] = ""
			break
		}
	}

	meta.keyState, meta.keySpecs, meta.keyPlanComplete = deriveRoutingKeySpecs(info)
	if name == "migrate" && len(info.KeySpecs) == 2 && len(meta.keySpecs) == 2 {
		key, keys := &meta.keySpecs[0], &meta.keySpecs[1]
		if key.begin == routingBeginIndex && key.index == 3 && key.find == routingFindRange &&
			key.lastKey == 0 && key.step == 1 && key.limit == 0 &&
			keys.begin == routingBeginKeyword && strings.EqualFold(keys.keyword, "keys") && keys.startFrom == -2 &&
			keys.find == routingFindRange && keys.lastKey == -1 && keys.step == 1 && keys.limit == 0 {
			// KEYS can also be a key or an AUTH operand. Reuse the option
			// parser for the known layout without overriding other metadata.
			keys.begin = routingBeginMigrate
		}
	}
	if len(info.KeySpecs) == 1 && len(meta.keySpecs) == 1 {
		spec := &meta.keySpecs[0]
		if spec.begin == routingBeginKeyword && strings.EqualFold(spec.keyword, "streams") &&
			spec.find == routingFindRange && spec.lastKey == -1 && spec.step == 1 && spec.limit == 2 &&
			(name == "xread" && spec.startFrom == 1 || name == "xreadgroup" && spec.startFrom == 4) {
			// STREAMS may also be a group/consumer name. Parse options to find
			// the real delimiter without changing the metadata's fan-out safety.
			spec.begin = routingBeginStreams
		}
	}
	if !respSet && meta.keyState == routingKeysKnown {
		resp = routing.RespDefaultHashSlot
	} else if !respSet && meta.keyState == routingKeysUnknown &&
		(len(info.KeySpecs) > 0 || info.FirstKeyPos > 0) {
		// Preserve a keyed response default, but disable multi-shard below.
		resp = routing.RespDefaultHashSlot
	}
	if meta.keyState == routingKeysNone && routingLocalSpecialCommand(name) &&
		req != routing.ReqMultiShard &&
		(resp == routing.RespDefaultKeyless || resp == routing.RespSpecial) {
		// Server tips may broadcast these diagnostics, but there is no
		// supported cluster-wide interpretation of their replies or cursors.
		// Preserve the known local forms without accepting a keyed split.
		req, resp = routing.ReqDefault, routing.RespDefaultKeyless
	}
	meta.policy = &routing.CommandPolicy{Request: req, Response: resp, Tips: tips}
	return meta
}

func deriveRoutingKeySpecs(info *CommandInfo) (routingKeyState, []routingKeySpec, bool) {
	if len(info.KeySpecs) == 0 {
		switch {
		case info.FirstKeyPos == 0 && info.LastKeyPos == 0 && info.StepCount == 0:
			return routingKeysNone, nil, true
		case info.FirstKeyPos > 0 && info.LastKeyPos != 0 && info.StepCount > 0:
			last := int(info.LastKeyPos)
			if last > 0 {
				if last < int(info.FirstKeyPos) {
					return routingKeysUnknown, nil, false
				}
				last -= int(info.FirstKeyPos)
			}
			return routingKeysKnown, []routingKeySpec{{
				begin:   routingBeginIndex,
				find:    routingFindRange,
				index:   int(info.FirstKeyPos),
				lastKey: last,
				step:    int(info.StepCount),
			}}, true
		default:
			return routingKeysUnknown, nil, false
		}
	}

	specs := make([]routingKeySpec, 0, len(info.KeySpecs))
	complete := true
	for _, raw := range info.KeySpecs {
		flagSemantics := classifyCommandMetadataKeySpecFlags(raw.Flags)
		if !flagSemantics.routingUsable {
			complete = false
			continue
		}
		if !flagSemantics.planComplete {
			complete = false
		}

		spec := routingKeySpec{
			index: raw.Index, keyword: raw.Keyword, startFrom: raw.StartFrom,
			lastKey: raw.LastKey, step: raw.KeyStep, limit: raw.Limit,
			keyNumIdx: raw.KeyNumIdx, firstKey: raw.FirstKey,
		}
		switch raw.BeginSearch {
		case "index":
			if raw.Index <= 0 {
				complete = false
				continue
			}
			spec.begin = routingBeginIndex
		case "keyword":
			if raw.Keyword == "" || raw.StartFrom == 0 {
				complete = false
				continue
			}
			spec.begin = routingBeginKeyword
		default:
			complete = false
			continue
		}
		switch raw.FindKeys {
		case "range":
			if raw.KeyStep <= 0 || raw.Limit < 0 {
				complete = false
				continue
			}
			// A limited range proves its first key, not a complete split plan.
			if raw.Limit != 0 {
				complete = false
			}
			spec.find = routingFindRange
		case "keynum":
			if raw.KeyStep <= 0 || raw.KeyNumIdx < 0 || raw.FirstKey <= 0 {
				complete = false
				continue
			}
			spec.find = routingFindKeynum
		default:
			complete = false
			continue
		}
		specs = append(specs, spec)
	}
	if len(specs) == 0 {
		return routingKeysUnknown, nil, false
	}
	return routingKeysKnown, specs, complete
}

// routingLookupMeta resolves exact wire command and subcommand tokens.
func routingLookupMeta(view *commandMetadataView, cmd Cmder) (*routingCommandMeta, bool) {
	meta, state := routingLookupMetaState(view, cmd)
	return meta, state == routingMetadataUsable
}

func routingLookupMetaState(
	view *commandMetadataView,
	cmd Cmder,
) (*routingCommandMeta, routingMetadataState) {
	if view == nil || len(cmd.Args()) == 0 || !routingCommandToken(cmd.Args()[0]) {
		return nil, routingMetadataMissing
	}
	name := cmd.Name()
	if _, parent := view.subcommandParents[name]; parent {
		if len(cmd.Args()) > 1 {
			if !routingCommandToken(cmd.Args()[1]) {
				return nil, routingMetadataMissing
			}
			child := internal.ToLower(cmd.stringArg(1))
			if child == "" {
				return nil, routingMetadataMissing
			}
			childName := name + "|" + child
			if meta, ok := view.routingTable[childName]; ok {
				return meta, routingMetadataUsable
			}
			if _, tombstoned := view.tombstones[childName]; tombstoned {
				return nil, routingMetadataUnusable
			}
			if _, resolved := view.records[childName]; resolved {
				return nil, routingMetadataUnusable
			}
			return nil, routingMetadataMissing
		}
	}
	if meta, ok := view.routingTable[name]; ok {
		return meta, routingMetadataUsable
	}
	if _, tombstoned := view.tombstones[name]; tombstoned {
		return nil, routingMetadataUnusable
	}
	if _, resolved := view.records[name]; resolved {
		return nil, routingMetadataUnusable
	}
	return nil, routingMetadataMissing
}

func routingCommandToken(arg interface{}) bool {
	switch value := arg.(type) {
	case string, []byte:
		return true
	case *string:
		return value != nil
	default:
		return false
	}
}

// routingPolicyFor rejects special policies without implemented handlers.
func routingPolicyFor(meta *routingCommandMeta) (*routing.CommandPolicy, bool) {
	if meta == nil || !meta.valid || meta.policy == nil {
		return nil, false
	}
	if meta.policy.Request == routing.ReqSpecial &&
		meta.special&(routingSpecialRequestDeclared|routingSpecialRequestSupported) !=
			routingSpecialRequestDeclared|routingSpecialRequestSupported {
		return nil, false
	}
	if meta.policy.Response == routing.RespSpecial &&
		meta.special&(routingSpecialResponseDeclared|routingSpecialResponseSupported) !=
			routingSpecialResponseDeclared|routingSpecialResponseSupported {
		return nil, false
	}
	if meta.policy.Request == routing.ReqMultiShard &&
		(meta.keyState != routingKeysKnown || !meta.keyPlanComplete) {
		return nil, false
	}
	return meta.policy, true
}

// routingFirstKeyPos returns a proven key position, or zero for proven keyless.
// Multi-shard routing uses routingResolveKeyPlan.
func routingFirstKeyPos(meta *routingCommandMeta, cmd Cmder) (pos int, ok bool) {
	if meta == nil || !meta.valid {
		return 0, false
	}
	if meta.keyState == routingKeysNone {
		return 0, true
	}
	if meta.keyState != routingKeysKnown || len(meta.keySpecs) == 0 {
		return 0, false
	}

	first := len(cmd.Args())
	firstNonEmpty := len(cmd.Args())
	matchedSpec := false
	matchedKey := false
	for _, spec := range meta.keySpecs {
		candidate, matched, valid := routingFirstKeyForSpec(spec, cmd)
		if !valid {
			return 0, false
		}
		if !matched {
			continue
		}
		matchedSpec = true
		if candidate == 0 {
			continue
		}
		matchedKey = true
		if candidate < first {
			first = candidate
		}
		key, wireOK := routingArgText(cmd, candidate)
		if !wireOK {
			return 0, false
		}
		if key != "" && candidate < firstNonEmpty {
			firstNonEmpty = candidate
		}
	}
	if !matchedSpec {
		// RedisJSON advertises MEMORY's key spec on the shared JSON.DEBUG
		// record, although its HELP variant takes no key.
		if meta.name == "json.debug" && len(cmd.Args()) == 2 {
			child, ok := routingArgText(cmd, 1)
			return 0, ok && strings.EqualFold(child, "help")
		}
		// An unmatched keyed form is not proven keyless.
		return 0, false
	}
	if !matchedKey {
		return 0, true
	}
	// Prefer a non-empty key when alternative specs match an empty placeholder.
	if firstNonEmpty < len(cmd.Args()) {
		return firstNonEmpty, true
	}
	return first, true
}

func routingFirstKeyForSpec(spec routingKeySpec, cmd Cmder) (int, bool, bool) {
	if spec.find == routingFindRange && spec.limit != 0 {
		begin, matched, ok := routingBeginPosition(spec, cmd)
		if !ok || !matched {
			return 0, matched, ok
		}
		if begin <= 0 || begin >= len(cmd.Args()) {
			return 0, true, false
		}
		return begin, true, true
	}
	layout, matched, ok := routingResolveKeySpecLayout(spec, cmd)
	if !ok || !matched {
		return 0, matched, ok
	}
	if layout.count == 0 {
		return 0, true, true
	}
	return layout.first, true, true
}

// routingResolveTransactionKeyPlan checks every key when the layout is known.
// Incomplete metadata may still prove a routing key; Redis validates the other
// keys when it queues the command, preserving transactions such as SORT.
func routingResolveTransactionKeyPlan(meta *routingCommandMeta, cmd Cmder) (routingKeyPlan, bool) {
	if meta == nil {
		return routingKeyPlan{numKeysPos: -1}, false
	}
	if len(meta.keySpecs) == 1 && meta.keySpecs[0].begin == routingBeginStreams {
		// Stream keys can be checked for one slot, but keys and IDs cannot
		// be split by fan-out. The view's metadata remains immutable.
		streams := *meta
		streams.keyPlanComplete = true
		meta = &streams
	}
	if !meta.keyPlanComplete {
		if first, ok := routingFirstKeyPos(meta, cmd); ok && first > 0 {
			return routingKeyPlan{positions: []int{first}, numKeysPos: -1}, true
		}
	}
	return routingResolveKeyPlan(meta, cmd)
}

// routingResolveKeyPlan returns all key positions or no plan.
func routingResolveKeyPlan(meta *routingCommandMeta, cmd Cmder) (routingKeyPlan, bool) {
	plan := routingKeyPlan{numKeysPos: -1}
	if meta == nil || !meta.valid {
		return plan, false
	}
	if meta.keyState == routingKeysNone {
		return plan, true
	}
	if meta.keyState != routingKeysKnown || len(meta.keySpecs) == 0 {
		return plan, false
	}
	if !meta.keyPlanComplete {
		return plan, false
	}

	for _, spec := range meta.keySpecs {
		positions, keyArgsEnd, numKeysPos, matched, ok := routingResolveKeySpec(spec, cmd)
		if !ok {
			return routingKeyPlan{numKeysPos: -1}, false
		}
		if !matched {
			continue
		}
		if plan.positions == nil {
			plan.positions = positions
		} else {
			plan.positions = append(plan.positions, positions...)
		}
		if len(meta.keySpecs) == 1 {
			plan.keyArgsEnd, plan.step, plan.numKeysPos = keyArgsEnd, spec.step, numKeysPos
			plan.splittable = spec.limit == 0
		}
	}
	slices.Sort(plan.positions)
	plan.positions = slices.Compact(plan.positions)
	for _, pos := range plan.positions {
		if _, ok := routingArgText(cmd, pos); !ok {
			return routingKeyPlan{numKeysPos: -1}, false
		}
	}
	return plan, true
}

func routingResolveKeySpec(spec routingKeySpec, cmd Cmder) ([]int, int, int, bool, bool) {
	layout, matched, ok := routingResolveKeySpecLayout(spec, cmd)
	if !ok || !matched {
		return nil, 0, -1, matched, ok
	}
	if layout.count == 0 {
		return nil, layout.keyArgsEnd, layout.numKeysPos, true, true
	}
	positions := make([]int, layout.count)
	for i := range layout.count {
		positions[i] = layout.first + i*layout.step
	}
	return positions, layout.keyArgsEnd, layout.numKeysPos, true, true
}

type routingKeySpecLayout struct {
	first      int
	count      int
	step       int
	keyArgsEnd int
	numKeysPos int
}

func routingResolveKeySpecLayout(spec routingKeySpec, cmd Cmder) (routingKeySpecLayout, bool, bool) {
	var layout routingKeySpecLayout
	layout.numKeysPos = -1
	argsLen := len(cmd.Args())
	begin, matched, ok := routingBeginPosition(spec, cmd)
	if !ok || !matched {
		return layout, matched, ok
	}

	switch spec.find {
	case routingFindRange:
		last := spec.lastKey
		if spec.limit != 0 {
			remaining := argsLen - begin
			if last != -1 || remaining <= 0 || remaining%spec.limit != 0 {
				return layout, true, false
			}
			last = begin + remaining/spec.limit - 1
		} else if last >= 0 {
			last += begin
		} else {
			last += argsLen
		}
		if begin <= 0 || begin >= argsLen || last < begin || last >= argsLen {
			return layout, true, false
		}
		layout.first = begin
		layout.count = (last-begin)/spec.step + 1
		layout.step = spec.step
		lastPos := begin + (layout.count-1)*spec.step
		if spec.step > argsLen-lastPos {
			layout.keyArgsEnd = argsLen
		} else {
			layout.keyArgsEnd = lastPos + spec.step
		}
		return layout, true, true

	case routingFindKeynum:
		layout.numKeysPos = begin + spec.keyNumIdx
		firstKey := begin + spec.firstKey
		if layout.numKeysPos <= 0 || layout.numKeysPos >= argsLen || firstKey <= 0 || firstKey > argsLen {
			return routingKeySpecLayout{numKeysPos: -1}, true, false
		}
		raw, wireOK := routingArgText(cmd, layout.numKeysPos)
		// Redis rejects signs and leading zeros in nonnegative counts. Splitting
		// must not rewrite an invalid count into valid per-shard commands.
		if !wireOK || len(raw) == 0 || raw[0] < '0' || raw[0] > '9' || len(raw) > 1 && raw[0] == '0' {
			return routingKeySpecLayout{numKeysPos: -1}, true, false
		}
		numKeys, err := strconv.Atoi(raw)
		if err != nil || numKeys < 0 {
			return routingKeySpecLayout{numKeysPos: -1}, true, false
		}
		layout.first = firstKey
		layout.count = numKeys
		layout.step = spec.step
		if numKeys == 0 {
			layout.keyArgsEnd = firstKey
			return layout, true, true
		}
		if firstKey >= argsLen || numKeys > argsLen || (argsLen-1-firstKey)/spec.step < numKeys-1 {
			return routingKeySpecLayout{numKeysPos: -1}, true, false
		}
		lastPos := firstKey + (numKeys-1)*spec.step
		if spec.step > argsLen-lastPos {
			return routingKeySpecLayout{numKeysPos: -1}, true, false
		}
		layout.keyArgsEnd = lastPos + spec.step
		return layout, true, true
	default:
		return layout, true, false
	}
}

func routingBeginPosition(spec routingKeySpec, cmd Cmder) (int, bool, bool) {
	switch spec.begin {
	case routingBeginMigrate:
		pos, valid := migrateKeysPos(cmd)
		return pos, pos != 0, valid
	case routingBeginStreams:
		// Match Redis's xreadGetKeys: option values are not delimiters.
		for i := 1; i < len(cmd.Args()); i++ {
			arg, ok := routingArgText(cmd, i)
			if !ok {
				return 0, true, false
			}
			switch internal.ToLower(arg) {
			case "block", "count", "maxcount", "maxsize", "claim":
				i++
			case "group":
				i += 2
			case "noack":
			case "streams":
				return i + 1, true, i+1 < len(cmd.Args())
			default:
				return 0, true, false
			}
		}
		return 0, true, false
	case routingBeginIndex:
		if spec.index <= 0 || spec.index >= len(cmd.Args()) {
			return 0, true, false
		}
		return spec.index, true, true
	case routingBeginKeyword:
		start := spec.startFrom
		if start < 0 {
			start += len(cmd.Args())
			if start <= 0 || start >= len(cmd.Args()) {
				return 0, false, true
			}
			for i := start; i > 0; i-- {
				arg, ok := routingArgText(cmd, i)
				if !ok {
					return 0, false, false
				}
				if strings.EqualFold(arg, spec.keyword) {
					if i+1 >= len(cmd.Args()) {
						return 0, true, false
					}
					return i + 1, true, true
				}
			}
			return 0, false, true
		}
		if start <= 0 || start >= len(cmd.Args()) {
			return 0, false, true
		}
		for i := start; i < len(cmd.Args()); i++ {
			arg, ok := routingArgText(cmd, i)
			if !ok {
				return 0, false, false
			}
			if strings.EqualFold(arg, spec.keyword) {
				if i+1 >= len(cmd.Args()) {
					return 0, true, false
				}
				return i + 1, true, true
			}
		}
		return 0, false, true
	default:
		return 0, false, false
	}
}

// routingArgText reproduces supported wire encodings; unknown types fail closed.
func routingArgText(cmd Cmder, pos int) (string, bool) {
	if pos < 0 || pos >= len(cmd.Args()) {
		return "", false
	}
	switch value := cmd.Args()[pos].(type) {
	case nil:
		return "", true
	case string:
		return value, true
	case *string:
		if value == nil {
			return "", true
		}
		return *value, true
	case []byte:
		return string(value), true
	case int:
		return strconv.FormatInt(int64(value), 10), true
	case *int:
		if value == nil {
			return "0", true
		}
		return strconv.FormatInt(int64(*value), 10), true
	case int8:
		return strconv.FormatInt(int64(value), 10), true
	case *int8:
		if value == nil {
			return "0", true
		}
		return strconv.FormatInt(int64(*value), 10), true
	case int16:
		return strconv.FormatInt(int64(value), 10), true
	case *int16:
		if value == nil {
			return "0", true
		}
		return strconv.FormatInt(int64(*value), 10), true
	case int32:
		return strconv.FormatInt(int64(value), 10), true
	case *int32:
		if value == nil {
			return "0", true
		}
		return strconv.FormatInt(int64(*value), 10), true
	case int64:
		return strconv.FormatInt(value, 10), true
	case *int64:
		if value == nil {
			return "0", true
		}
		return strconv.FormatInt(*value, 10), true
	case uint:
		return strconv.FormatUint(uint64(value), 10), true
	case *uint:
		if value == nil {
			return "0", true
		}
		return strconv.FormatUint(uint64(*value), 10), true
	case uint8:
		return strconv.FormatUint(uint64(value), 10), true
	case *uint8:
		if value == nil {
			return "0", true
		}
		return strconv.FormatUint(uint64(*value), 10), true
	case uint16:
		return strconv.FormatUint(uint64(value), 10), true
	case *uint16:
		if value == nil {
			return "0", true
		}
		return strconv.FormatUint(uint64(*value), 10), true
	case uint32:
		return strconv.FormatUint(uint64(value), 10), true
	case *uint32:
		if value == nil {
			return "0", true
		}
		return strconv.FormatUint(uint64(*value), 10), true
	case uint64:
		return strconv.FormatUint(value, 10), true
	case *uint64:
		if value == nil {
			return "0", true
		}
		return strconv.FormatUint(*value, 10), true
	case uintptr:
		return strconv.FormatUint(uint64(value), 10), true
	case *uintptr:
		if value == nil {
			return "0", true
		}
		return strconv.FormatUint(uint64(*value), 10), true
	case float32:
		return strconv.FormatFloat(float64(value), 'f', -1, 64), true
	case *float32:
		if value == nil {
			return "0", true
		}
		return strconv.FormatFloat(float64(*value), 'f', -1, 64), true
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64), true
	case *float64:
		if value == nil {
			return "0", true
		}
		return strconv.FormatFloat(*value, 'f', -1, 64), true
	case bool:
		if value {
			return "1", true
		}
		return "0", true
	case *bool:
		if value != nil && *value {
			return "1", true
		}
		return "0", true
	case time.Time:
		return value.Format(time.RFC3339Nano), true
	case *time.Time:
		if value == nil {
			value = &time.Time{}
		}
		return value.Format(time.RFC3339Nano), true
	case time.Duration:
		return strconv.FormatInt(value.Nanoseconds(), 10), true
	case *time.Duration:
		if value == nil {
			return "0", true
		}
		return strconv.FormatInt(value.Nanoseconds(), 10), true
	case net.IP:
		return string(value), true
	default:
		return "", false
	}
}
