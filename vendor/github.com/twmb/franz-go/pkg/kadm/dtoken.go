package kadm

import (
	"context"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// Principal is a principal that owns or renews a delegation token. This is the
// same as an ACL's principal, but rather than being a single string, the type
// and name are split into two fields.
type Principal struct {
	Type string // Type is the type of a principal owner or renewer. If empty, this defaults to "User".
	Name string // Name is the name of a principal owner or renewer.
}

// DelegationToken contains information about a delegation token.
type DelegationToken struct {
	// Owner is the owner of the delegation token.
	Owner Principal
	// TokenRequesterPrincipal is the principal of the creator of the
	// token. This exists for v3+, where you can override the owner.
	// For prior than v3, this is just the Owner.
	TokenRequesterPrincipal Principal
	// IssueTimestamp is timestamp the delegation token creation request
	// is received within the broker.
	IssueTimestamp time.Time
	// ExpiryTimestamp is the timestamp the delegation token will expire.
	// This field is:
	//     min(MaxTimestamp, IssueTimestamp+delegation.token.expiry.time.ms)
	// where the default expiry is 24hr.
	ExpiryTimestamp time.Time
	// MaxTimestamp is the timestamp past which the delegation token cannot
	// be renewed. This is either the requested MaxLifetime, or the
	// broker's delegation.token.max.lifetime.ms which is 7d by default.
	MaxTimestamp time.Time
	// TokenID is the username of this token for use in authorization.
	TokenID string
	// HMAC is the password of this token for use for in authorization.
	HMAC []byte
	// Renewers is the list of principals that can renew this token in
	// addition to the owner (which always can).
	Renewers []Principal
}

// DelegationTokens contains a list of delegation tokens.
type DelegationTokens []DelegationToken

// CreateDelegationToken is a create delegation token request, allowing you to
// create scoped tokens with the same ACLs as the creator. This allows you to
// more easily manage authorization for a wide array of clients. All delegation
// tokens use SCRAM-SHA-256 SASL for authorization.
type CreateDelegationToken struct {
	// Owner overrides the owner of the token from the principal issuing
	// the request to the principal in this field. This allows a superuser
	// to create tokens without requiring individual user credentials, and
	// for a superuser to run clients on behalf of another user. These
	// fields require Kafka 3.3+; see KIP-373 for more details.
	Owner *Principal
	// Renewers is a list of principals that can renew the delegation
	// token in addition to the owner of the token. This list does not
	// include the owner.
	Renewers []Principal
	// MaxLifetime is how long the delegation token is valid for.
	// If -1, the default is the server's delegation.token.max.lifetime.ms,
	// which is by default 7d.
	MaxLifetime time.Duration
}

// CreateDelegationToken creates a delegation token, which is a scoped
// SCRAM-SHA-256 username and password.
//
// Creating delegation tokens allows for an (ideally) quicker and easier method
// of enabling authorization for a wide array of clients. Rather than having to
// manage many passwords external to Kafka, you only need to manage a few
// accounts and use those to create delegation tokens per client.
//
// Note that delegation tokens inherit the same ACLs as the user creating the
// token. Thus, if you want to properly scope ACLs, you should not create
// delegation tokens with admin accounts.
//
// This can return *AuthError.
func (cl *Client) CreateDelegationToken(ctx context.Context, d CreateDelegationToken) (DelegationToken, error) {
	req := kmsg.NewPtrCreateDelegationTokenRequest()
	if d.Owner != nil {
		req.OwnerPrincipalType = &d.Owner.Type
		req.OwnerPrincipalName = &d.Owner.Name
	}
	for _, renewer := range d.Renewers {
		rr := kmsg.NewCreateDelegationTokenRequestRenewer()
		rr.PrincipalType = renewer.Type
		rr.PrincipalName = renewer.Name
		req.Renewers = append(req.Renewers, rr)
	}
	req.MaxLifetimeMillis = d.MaxLifetime.Milliseconds()
	resp, err := req.RequestWith(ctx, cl.cl)
	if err != nil {
		return DelegationToken{}, err
	}
	if err := maybeAuthErr(resp.ErrorCode); err != nil {
		return DelegationToken{}, err
	}
	if err := kerr.ErrorForCode(resp.ErrorCode); err != nil {
		return DelegationToken{}, err
	}

	t := DelegationToken{
		Owner: Principal{
			Type: resp.PrincipalType,
			Name: resp.PrincipalName,
		},
		TokenRequesterPrincipal: Principal{
			Type: resp.TokenRequesterPrincipalType,
			Name: resp.TokenRequesterPrincipalName,
		},
		IssueTimestamp:  time.UnixMilli(resp.IssueTimestamp).UTC(),
		ExpiryTimestamp: time.UnixMilli(resp.ExpiryTimestamp).UTC(),
		MaxTimestamp:    time.UnixMilli(resp.MaxTimestamp).UTC(),
		TokenID:         resp.TokenID,
		HMAC:            resp.HMAC,
		Renewers:        append([]Principal(nil), d.Renewers...),
	}
	if resp.Version < 3 {
		t.TokenRequesterPrincipal = t.Owner
	}
	return t, nil
}

// RenewDelegationToken renews a delegation token that has not yet hit its max
// timestamp and returns the new expiry timestamp.
//
// This can return *AuthError.
func (cl *Client) RenewDelegationToken(ctx context.Context, hmac []byte, renewTime time.Duration) (expiryTimestamp time.Time, err error) {
	req := kmsg.NewPtrRenewDelegationTokenRequest()
	req.HMAC = hmac
	req.RenewTimeMillis = renewTime.Milliseconds()
	resp, err := req.RequestWith(ctx, cl.cl)
	if err != nil {
		return time.Time{}, err
	}
	if err := maybeAuthErr(resp.ErrorCode); err != nil {
		return time.Time{}, err
	}
	if err := kerr.ErrorForCode(resp.ErrorCode); err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(resp.ExpiryTimestamp).UTC(), nil
}

// ExpireDelegationToken changes a delegation token's expiry timestamp and
// returns the new expiry timestamp, which is min(now+expiry, maxTimestamp).
// This request can be used to force tokens to expire quickly, or to give
// tokens a grace period before expiry. Using an expiry of -1 expires the token
// immediately.
//
// This can return *AuthError.
func (cl *Client) ExpireDelegationToken(ctx context.Context, hmac []byte, expiry time.Duration) (expiryTimestamp time.Time, err error) {
	req := kmsg.NewPtrExpireDelegationTokenRequest()
	req.HMAC = hmac
	req.ExpiryPeriodMillis = expiry.Milliseconds()
	resp, err := req.RequestWith(ctx, cl.cl)
	if err != nil {
		return time.Time{}, err
	}
	if err := maybeAuthErr(resp.ErrorCode); err != nil {
		return time.Time{}, err
	}
	if err := kerr.ErrorForCode(resp.ErrorCode); err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(resp.ExpiryTimestamp).UTC(), nil
}

// DescribeDelegationTokens describes delegation tokens. This returns either
// all delegation tokens, or returns only tokens with owners in the requested
// owners list.
//
// This can return *AuthError.
func (cl *Client) DescribeDelegationTokens(ctx context.Context, owners ...Principal) (DelegationTokens, error) {
	req := kmsg.NewPtrDescribeDelegationTokenRequest()
	for _, owner := range owners {
		ro := kmsg.NewDescribeDelegationTokenRequestOwner()
		ro.PrincipalType = owner.Type
		ro.PrincipalName = owner.Name
		req.Owners = append(req.Owners, ro)
	}
	resp, err := req.RequestWith(ctx, cl.cl)
	if err != nil {
		return nil, err
	}
	if err := maybeAuthErr(resp.ErrorCode); err != nil {
		return nil, err
	}
	if err := kerr.ErrorForCode(resp.ErrorCode); err != nil {
		return nil, err
	}

	var ts DelegationTokens
	for _, d := range resp.TokenDetails {
		t := DelegationToken{
			Owner: Principal{
				Type: d.PrincipalType,
				Name: d.PrincipalName,
			},
			TokenRequesterPrincipal: Principal{
				Type: d.TokenRequesterPrincipalType,
				Name: d.TokenRequesterPrincipalName,
			},
			IssueTimestamp:  time.UnixMilli(d.IssueTimestamp).UTC(),
			ExpiryTimestamp: time.UnixMilli(d.ExpiryTimestamp).UTC(),
			MaxTimestamp:    time.UnixMilli(d.MaxTimestamp).UTC(),
			TokenID:         d.TokenID,
			HMAC:            d.HMAC,
		}
		if resp.Version < 3 {
			t.TokenRequesterPrincipal = t.Owner
		}
		for _, r := range d.Renewers {
			t.Renewers = append(t.Renewers, Principal{
				Type: r.PrincipalType,
				Name: r.PrincipalName,
			})
		}
		ts = append(ts, t)
	}
	return ts, nil
}
