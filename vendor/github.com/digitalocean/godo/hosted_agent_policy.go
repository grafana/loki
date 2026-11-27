package godo

import (
	"bytes"
	"context"
	"errors"
)

// HostedAgentPolicyAction is a tool-permission disposition.
type HostedAgentPolicyAction string

const (
	HostedAgentPolicyActionAllow HostedAgentPolicyAction = "allow"
	HostedAgentPolicyActionAsk   HostedAgentPolicyAction = "ask"
	HostedAgentPolicyActionDeny  HostedAgentPolicyAction = "deny"
)

// HostedAgentPolicyEnforcement controls how strictly a rule is enforced.
type HostedAgentPolicyEnforcement string

const (
	HostedAgentPolicyEnforcementStrict     HostedAgentPolicyEnforcement = "strict"
	HostedAgentPolicyEnforcementBestEffort HostedAgentPolicyEnforcement = "best-effort"
)

// HostedAgentPolicyVerdict is the validation outcome for one resolved rule.
type HostedAgentPolicyVerdict string

const (
	HostedAgentPolicyVerdictExact       HostedAgentPolicyVerdict = "exact"
	HostedAgentPolicyVerdictDegraded    HostedAgentPolicyVerdict = "degraded"
	HostedAgentPolicyVerdictUnsupported HostedAgentPolicyVerdict = "unsupported"
	HostedAgentPolicyVerdictUnverified  HostedAgentPolicyVerdict = "unverified"
	HostedAgentPolicyVerdictDelegated   HostedAgentPolicyVerdict = "delegated"
)

// HostedAgentPolicyResolvedRule is a rule after resolution, carrying a stable
// id for audit correlation.
type HostedAgentPolicyResolvedRule struct {
	ID          string                       `json:"id,omitempty"`
	Tool        string                       `json:"tool,omitempty"`
	Match       map[string]string            `json:"match,omitempty"`
	Action      HostedAgentPolicyAction      `json:"action,omitempty"`
	Enforcement HostedAgentPolicyEnforcement `json:"enforcement,omitempty"`
}

// HostedAgentPolicyRuleVerdict pairs a resolved rule with its validation outcome.
type HostedAgentPolicyRuleVerdict struct {
	Rule       HostedAgentPolicyResolvedRule `json:"rule,omitempty"`
	Verdict    HostedAgentPolicyVerdict      `json:"verdict,omitempty"`
	Rendered   HostedAgentPolicyAction       `json:"rendered,omitempty"`
	Reason     string                        `json:"reason,omitempty"`
	Suggestion string                        `json:"suggestion,omitempty"`
}

// HostedAgentPolicyValidationResult is returned by ValidatePolicy.
// The same shape is persisted per session in session_policies.verdicts.
type HostedAgentPolicyValidationResult struct {
	Agent   string `json:"agent,omitempty"`
	Version string `json:"version,omitempty"`
	OK      bool   `json:"ok"`
	// DefaultAction is the catch-all disposition as shipped (after unverified
	// allow→ask downgrade). When DefaultActionVerdict is degraded, this remains
	// the shipped value; see DefaultActionRendered for what the agent enforces.
	DefaultAction HostedAgentPolicyAction `json:"defaultAction,omitempty"`
	// DefaultActionVerdict classifies how faithfully the agent can enforce the
	// catch-all defaultAction.
	DefaultActionVerdict HostedAgentPolicyVerdict `json:"defaultActionVerdict,omitempty"`
	// DefaultActionRendered is the disposition the agent will actually enforce
	// as the catch-all.
	DefaultActionRendered HostedAgentPolicyAction        `json:"defaultActionRendered,omitempty"`
	DefaultActionReason   string                         `json:"defaultActionReason,omitempty"`
	Verdicts              []HostedAgentPolicyRuleVerdict `json:"verdicts,omitempty"`
	// ActionGatewayReason is set when a do.actions tool/toolbelt reference failed
	// tool-registry's existence check.
	ActionGatewayReason string `json:"actionGatewayReason,omitempty"`
}

// ValidatePolicy checks a manifest's tool-permission policy against the agent's
// capabilities without creating a session.
func (s *HostedAgentsServiceOp) ValidatePolicy(ctx context.Context, manifest []byte) (*HostedAgentPolicyValidationResult, *Response, error) {
	if len(bytes.TrimSpace(manifest)) == 0 {
		return nil, nil, errors.New("hosted agents: manifest is required")
	}
	req, err := s.newCreateSessionPostRequest(ctx, hostedAgentPolicyValidatePath, bytes.NewReader(manifest), hostedAgentManifestMediaType)
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentPolicyValidationResult)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}
