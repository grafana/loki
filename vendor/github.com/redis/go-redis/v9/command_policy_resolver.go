package redis

import (
	"context"
	"strings"

	"github.com/redis/go-redis/v9/internal/routing"
)

type (
	module      = string
	commandName = string
)

var defaultPolicies = map[module]map[commandName]*routing.CommandPolicy{
	"ft": {
		"create": {
			Request:  routing.ReqDefault,
			Response: routing.RespDefaultKeyless,
		},
		"search": {
			Request:  routing.ReqDefault,
			Response: routing.RespDefaultKeyless,
			Tips: map[string]string{
				routing.ReadOnlyCMD: "",
			},
		},
		"aggregate": {
			Request:  routing.ReqDefault,
			Response: routing.RespDefaultKeyless,
			Tips: map[string]string{
				routing.ReadOnlyCMD: "",
			},
		},
		"dictadd": {
			Request:  routing.ReqDefault,
			Response: routing.RespDefaultKeyless,
		},
		"dictdump": {
			Request:  routing.ReqDefault,
			Response: routing.RespDefaultKeyless,
			Tips: map[string]string{
				routing.ReadOnlyCMD: "",
			},
		},
		"dictdel": {
			Request:  routing.ReqDefault,
			Response: routing.RespDefaultKeyless,
		},
		"suglen": {
			Request:  routing.ReqDefault,
			Response: routing.RespDefaultHashSlot,
			Tips: map[string]string{
				routing.ReadOnlyCMD: "",
			},
		},
		"cursor": {
			Request:  routing.ReqSpecial,
			Response: routing.RespDefaultKeyless,
			Tips: map[string]string{
				routing.ReadOnlyCMD: "",
			},
		},
		"sugadd": {
			Request:  routing.ReqDefault,
			Response: routing.RespDefaultHashSlot,
		},
		"sugget": {
			Request:  routing.ReqDefault,
			Response: routing.RespDefaultHashSlot,
			Tips: map[string]string{
				routing.ReadOnlyCMD: "",
			},
		},
		"sugdel": {
			Request:  routing.ReqDefault,
			Response: routing.RespDefaultHashSlot,
		},
		"spellcheck": {
			Request:  routing.ReqDefault,
			Response: routing.RespDefaultKeyless,
			Tips: map[string]string{
				routing.ReadOnlyCMD: "",
			},
		},
		"explain": {
			Request:  routing.ReqDefault,
			Response: routing.RespDefaultKeyless,
			Tips: map[string]string{
				routing.ReadOnlyCMD: "",
			},
		},
		"explaincli": {
			Request:  routing.ReqDefault,
			Response: routing.RespDefaultKeyless,
			Tips: map[string]string{
				routing.ReadOnlyCMD: "",
			},
		},
		"aliasadd": {
			Request:  routing.ReqDefault,
			Response: routing.RespDefaultKeyless,
		},
		"aliasupdate": {
			Request:  routing.ReqDefault,
			Response: routing.RespDefaultKeyless,
		},
		"aliasdel": {
			Request:  routing.ReqDefault,
			Response: routing.RespDefaultKeyless,
		},
		"aliaslist": {
			Request:  routing.ReqDefault,
			Response: routing.RespDefaultKeyless,
			Tips: map[string]string{
				routing.ReadOnlyCMD: "",
			},
		},
		"info": {
			Request:  routing.ReqDefault,
			Response: routing.RespDefaultKeyless,
			Tips: map[string]string{
				routing.ReadOnlyCMD: "",
			},
		},
		"tagvals": {
			Request:  routing.ReqDefault,
			Response: routing.RespDefaultKeyless,
			Tips: map[string]string{
				routing.ReadOnlyCMD: "",
			},
		},
		"syndump": {
			Request:  routing.ReqDefault,
			Response: routing.RespDefaultKeyless,
			Tips: map[string]string{
				routing.ReadOnlyCMD: "",
			},
		},
		"synupdate": {
			Request:  routing.ReqDefault,
			Response: routing.RespDefaultKeyless,
		},
		"profile": {
			Request:  routing.ReqDefault,
			Response: routing.RespDefaultKeyless,
			Tips: map[string]string{
				routing.ReadOnlyCMD: "",
			},
		},
		"alter": {
			Request:  routing.ReqDefault,
			Response: routing.RespDefaultKeyless,
		},
		"dropindex": {
			Request:  routing.ReqDefault,
			Response: routing.RespDefaultKeyless,
		},
		"drop": {
			Request:  routing.ReqDefault,
			Response: routing.RespDefaultKeyless,
		},
	},
}

// CommandInfoResolveFunc resolves one command's Cluster routing policy.
// Nil delegates to the fallback resolver.
type CommandInfoResolveFunc func(ctx context.Context, cmd Cmder) *routing.CommandPolicy

type commandInfoResolver struct {
	resolveFunc      CommandInfoResolveFunc
	fallBackResolver *commandInfoResolver
	legacyDefault    bool

	// metadataView marks a resolver backed by shared COMMAND metadata.
	metadataView   func() *commandMetadataView
	metadataEnsure func(context.Context) error
}

type commandRoutingResolution struct {
	policy *routing.CommandPolicy
	// Also true when metadata was reached but its policy requires an
	// unsupported handler. Dispatch must validate that policy rather than
	// silently treating an explicit metadata opt-in as ordinary routing.
	policyFromMetadata bool
	meta               *routingCommandMeta
	metaOK             bool
}

func NewCommandInfoResolver(resolveFunc CommandInfoResolveFunc) *commandInfoResolver {
	return &commandInfoResolver{resolveFunc: resolveFunc}
}

// scanPolicy is the policy the server advertises for SCAN (request_policy:special,
// response_policy:special). The default resolver applies it too, so a SCAN
// iterator covers every master without opting into metadata policies.
var scanPolicy = &routing.CommandPolicy{
	Request:  routing.ReqSpecial,
	Response: routing.RespSpecial,
	Tips:     map[string]string{"readonly": ""},
}

// NewDefaultCommandPolicyResolver preserves the built-in Search routing policies
// and the SCAN policy. It does not enable new fan-out policies for other
// commands. Use NewCommandMetadataResolver to opt into metadata-based fan-out
// and aggregation.
func NewDefaultCommandPolicyResolver() *commandInfoResolver {
	r := NewCommandInfoResolver(func(_ context.Context, cmd Cmder) *routing.CommandPolicy {
		name := cmd.Name()
		if name == "scan" {
			return scanPolicy
		}
		dot := strings.IndexByte(name, '.')
		if dot < 0 || strings.IndexByte(name[dot+1:], '.') >= 0 {
			return nil
		}
		return defaultPolicies[name[:dot]][name[dot+1:]]
	})
	r.legacyDefault = true
	return r
}

// NewCommandMetadataResolver derives routing policies from the shared COMMAND
// metadata. Install it with ClusterClient.SetCommandInfoResolver to opt into
// fan-out and aggregation, including non-atomic cross-slot multi-key commands.
// A ClusterClient uses its current metadata view, including configured overrides
// and live metadata. Used directly, the resolver uses the shipped snapshot.
func NewCommandMetadataResolver() *commandInfoResolver {
	view := defaultCommandMetadataView()
	return newCommandMetadataPolicyResolver(func() *commandMetadataView {
		return view
	})
}

// defaultPolicyKeyless reports whether name (e.g. "ft.aliaslist") is registered
// in the static policy table as a plain keyless command: default request
// routing with a keyless response policy. Commands whose slot comes from a key
// (RespDefaultHashSlot, e.g. ft.suglen) or with special request routing
// (ReqSpecial, e.g. ft.cursor) are excluded — their key position must still be
// resolved. cmdFirstKeyPosWithInfo consults this so the initial slot
// computation on a cold command-info cache matches the policy the router
// applies once the command reaches routeAndRun.
func defaultPolicyKeyless(name string) bool {
	i := strings.IndexByte(name, '.')
	if i < 0 {
		return false
	}
	policy, ok := defaultPolicies[name[:i]][name[i+1:]]
	if !ok {
		return false
	}
	return policy.Request == routing.ReqDefault && policy.Response == routing.RespDefaultKeyless
}

// newCommandMetadataPolicyResolver loads the latest view for each invocation.
func newCommandMetadataPolicyResolver(view func() *commandMetadataView) *commandInfoResolver {
	return newCommandMetadataPolicyResolverWithEnsure(view, nil)
}

func newCommandMetadataPolicyResolverWithEnsure(
	view func() *commandMetadataView,
	ensure func(context.Context) error,
) *commandInfoResolver {
	r := &commandInfoResolver{metadataView: view, metadataEnsure: ensure}
	r.resolveFunc = func(ctx context.Context, cmd Cmder) *routing.CommandPolicy {
		// Direct dynamic resolvers fetch synchronously and fall back on failure.
		if ensure != nil {
			_ = ensure(ctx)
		}
		return r.commandPolicyInView(ctx, cmd, view())
	}
	return r
}

func (r *commandInfoResolver) GetCommandPolicy(ctx context.Context, cmd Cmder) *routing.CommandPolicy {
	if r == nil || r.resolveFunc == nil {
		return nil
	}
	if policy := r.resolveFunc(ctx, cmd); policy != nil {
		// Do not expose immutable view data to callers that may mutate it.
		if r.metadataView != nil {
			return cloneRoutingPolicy(policy)
		}
		return policy
	}
	if r.fallBackResolver != nil {
		return r.fallBackResolver.GetCommandPolicy(ctx, cmd)
	}
	return nil
}

func cloneRoutingPolicy(policy *routing.CommandPolicy) *routing.CommandPolicy {
	clone := *policy
	if policy.Tips != nil {
		clone.Tips = make(map[string]string, len(policy.Tips))
		for key, value := range policy.Tips {
			clone.Tips[key] = value
		}
	}
	return &clone
}

func (r *commandInfoResolver) SetFallbackResolver(fallbackResolver *commandInfoResolver) {
	r.fallBackResolver = fallbackResolver
}

// resolveCommandRoutingWithView resolves policy and metadata from one view.
func (r *commandInfoResolver) resolveCommandRoutingWithView(
	ctx context.Context,
	cmd Cmder,
	fallbackView func() *commandMetadataView,
) (commandRoutingResolution, *commandMetadataView, error) {
	var firstErr error
	var resolution commandRoutingResolution
	var view *commandMetadataView
	resolveMeta := func() {
		resolution.meta, resolution.metaOK = routingLookupMeta(view, cmd)
	}
	for current := r; current != nil; current = current.fallBackResolver {
		// A resolver without a function terminates the chain.
		if current.resolveFunc == nil {
			break
		}
		if current.metadataView != nil {
			resolution.policyFromMetadata = true
			if current.metadataEnsure != nil {
				if err := current.metadataEnsure(ctx); err != nil {
					// Keep routing with the current view and report the refresh error.
					if firstErr == nil {
						firstErr = err
					}
				}
			}
			// Prefer the client-owned view for a consistent invocation.
			view = nil
			if fallbackView != nil {
				view = fallbackView()
			}
			if view == nil {
				view = current.metadataView()
			}
			resolveMeta()
			if policy, ok := routingPolicyFor(resolution.meta); resolution.metaOK && ok {
				resolution.policy = policy
				return resolution, view, firstErr
			}
			continue
		}

		// Evaluate custom resolvers once, then capture key metadata.
		if policy := current.resolveFunc(ctx, cmd); policy != nil {
			if fallbackView != nil {
				if currentView := fallbackView(); currentView != nil {
					view = currentView
				}
			}
			resolveMeta()
			resolution.policy = policy
			resolution.policyFromMetadata = false
			return resolution, view, firstErr
		}
	}

	if fallbackView != nil {
		if currentView := fallbackView(); currentView != nil {
			view = currentView
		}
	}
	resolveMeta()
	return resolution, view, firstErr
}

func (r *commandInfoResolver) getCommandRoutingInView(
	ctx context.Context,
	cmd Cmder,
	view *commandMetadataView,
) commandRoutingResolution {
	meta, metaOK := routingLookupMeta(view, cmd)
	resolution := commandRoutingResolution{meta: meta, metaOK: metaOK}
	if r == nil || r.resolveFunc == nil {
		return resolution
	}
	for current := r; current != nil; current = current.fallBackResolver {
		if current.resolveFunc == nil {
			break
		}
		if current.metadataView != nil {
			resolution.policyFromMetadata = true
			if metaOK {
				resolution.policy, _ = routingPolicyFor(meta)
			}
		} else {
			resolution.policy = current.resolveFunc(ctx, cmd)
			if resolution.policy != nil {
				resolution.policyFromMetadata = false
			}
		}
		if resolution.policy != nil {
			break
		}
	}
	return resolution
}

// resolveCommandRoutingsWithView resolves a batch in one captured generation,
// invoking custom resolvers and metadata ensure hooks at most once as needed.
func (r *commandInfoResolver) resolveCommandRoutingsWithView(
	ctx context.Context,
	cmds []Cmder,
	fallbackView func() *commandMetadataView,
) ([]commandRoutingResolution, *commandMetadataView, error) {
	// Cache custom answers so a live refresh can restart metadata resolution
	// without invoking application callbacks a second time.
	type customLookup struct {
		resolver *commandInfoResolver
		index    int
	}
	var customAnswers map[customLookup]*routing.CommandPolicy
	var ensured map[*commandInfoResolver]struct{}
	// Without an ensure hook resolution cannot restart, so answers do not
	// need to be retained. The fallback chain can change between calls.
	for current := r; current != nil && current.resolveFunc != nil; current = current.fallBackResolver {
		if current.metadataView != nil && current.metadataEnsure != nil {
			customAnswers = make(map[customLookup]*routing.CommandPolicy)
			ensured = make(map[*commandInfoResolver]struct{})
			break
		}
	}
	resolutions := make([]commandRoutingResolution, len(cmds))
	var firstErr error
	var view *commandMetadataView
	captureView := func(resolver *commandInfoResolver) *commandMetadataView {
		if fallbackView != nil {
			if v := fallbackView(); v != nil {
				return v
			}
		}
		if resolver != nil {
			return resolver.metadataView()
		}
		return nil
	}

	for {
		restart := false
		for i, cmd := range cmds {
			resolution := commandRoutingResolution{}
			for current := r; current != nil; current = current.fallBackResolver {
				if current.resolveFunc == nil {
					break
				}
				if current.metadataView != nil {
					resolution.policyFromMetadata = true
					if _, done := ensured[current]; !done && current.metadataEnsure != nil {
						ensured[current] = struct{}{}
						if err := current.metadataEnsure(ctx); err != nil && firstErr == nil {
							firstErr = err
						}
						next := captureView(current)
						restart = view != nil && view != next
						view = next
						if restart {
							// Earlier commands must use the same refreshed keys and
							// policies as the command that reached this fallback.
							break
						}
					}
					if view == nil {
						view = captureView(current)
					}
					resolution.meta, resolution.metaOK = routingLookupMeta(view, cmd)
					if resolution.metaOK {
						resolution.policy, _ = routingPolicyFor(resolution.meta)
					}
				} else {
					key := customLookup{resolver: current, index: i}
					policy, known := customAnswers[key]
					if !known {
						policy = current.resolveFunc(ctx, cmd)
						if customAnswers != nil {
							customAnswers[key] = policy
						}
					}
					resolution.policy = policy
					if policy != nil {
						resolution.policyFromMetadata = false
					}
				}
				if resolution.policy != nil {
					break
				}
			}
			if restart {
				break
			}
			resolutions[i] = resolution
		}
		if restart {
			continue
		}
		if view == nil {
			view = captureView(nil)
		}
		for i, cmd := range cmds {
			if !resolutions[i].policyFromMetadata {
				resolutions[i].meta, resolutions[i].metaOK = routingLookupMeta(view, cmd)
			}
		}
		return resolutions, view, firstErr
	}
}

func (r *commandInfoResolver) commandPolicyInView(
	_ context.Context,
	cmd Cmder,
	view *commandMetadataView,
) *routing.CommandPolicy {
	meta, ok := routingLookupMeta(view, cmd)
	if !ok {
		return nil
	}
	policy, ok := routingPolicyFor(meta)
	if !ok {
		return nil
	}
	return policy
}
