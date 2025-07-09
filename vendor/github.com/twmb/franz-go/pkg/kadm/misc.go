package kadm

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"golang.org/x/crypto/pbkdf2"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
	"github.com/twmb/franz-go/pkg/kversion"
)

// ErrAndMessage is returned as the error from requests that were successfully
// responded to, but the response indicates failure with a message.
type ErrAndMessage struct {
	Err        error  // Err is the response ErrorCode.
	ErrMessage string // Message is the response ErrorMessage.
}

func (e *ErrAndMessage) Error() string {
	var ke *kerr.Error
	if errors.As(e.Err, &ke) && e.ErrMessage != "" {
		return ke.Message + ": " + e.ErrMessage
	}
	return e.Err.Error()
}

func (e *ErrAndMessage) Unwrap() error {
	return e.Err
}

// FindCoordinatorResponse contains information for the coordinator for a group
// or transactional ID.
type FindCoordinatorResponse struct {
	Name       string // Name is the coordinator key this response is for.
	NodeID     int32  // NodeID is the node ID of the coordinator for this key.
	Host       string // Host is the host of the coordinator for this key.
	Port       int32  // Port is the port of the coordinator for this key.
	Err        error  // Err is any error encountered when requesting the coordinator.
	ErrMessage string // ErrMessage a potential extra message describing any error.
}

// FindCoordinatorResponses contains responses to finding coordinators for
// groups or transactions.
type FindCoordinatorResponses map[string]FindCoordinatorResponse

// AllFailed returns whether all responses are errored.
func (rs FindCoordinatorResponses) AllFailed() bool {
	var n int
	rs.EachError(func(FindCoordinatorResponse) { n++ })
	return len(rs) > 0 && n == len(rs)
}

// Sorted returns all coordinator responses sorted by name.
func (rs FindCoordinatorResponses) Sorted() []FindCoordinatorResponse {
	s := make([]FindCoordinatorResponse, 0, len(rs))
	for _, r := range rs {
		s = append(s, r)
	}
	sort.Slice(s, func(i, j int) bool { return s[i].Name < s[j].Name })
	return s
}

// EachError calls fn for every response that has a non-nil error.
func (rs FindCoordinatorResponses) EachError(fn func(FindCoordinatorResponse)) {
	for _, r := range rs {
		if r.Err != nil {
			fn(r)
		}
	}
}

// Each calls fn for every response.
func (rs FindCoordinatorResponses) Each(fn func(FindCoordinatorResponse)) {
	for _, r := range rs {
		fn(r)
	}
}

// Error iterates over all responses and returns the first error encountered,
// if any.
func (rs FindCoordinatorResponses) Error() error {
	for _, r := range rs {
		if r.Err != nil {
			return r.Err
		}
	}
	return nil
}

// Ok returns true if there are no errors. This is a shortcut for rs.Error() ==
// nil.
func (rs FindCoordinatorResponses) Ok() bool {
	return rs.Error() == nil
}

// FindGroupCoordinators returns the coordinator for all requested group names.
//
// This may return *ShardErrors or *AuthError.
func (cl *Client) FindGroupCoordinators(ctx context.Context, groups ...string) FindCoordinatorResponses {
	return cl.findCoordinators(ctx, 0, groups...)
}

// FindTxnCoordinators returns the coordinator for all requested transactional
// IDs.
//
// This may return *ShardErrors or *AuthError.
func (cl *Client) FindTxnCoordinators(ctx context.Context, txnIDs ...string) FindCoordinatorResponses {
	return cl.findCoordinators(ctx, 1, txnIDs...)
}

func (cl *Client) findCoordinators(ctx context.Context, kind int8, names ...string) FindCoordinatorResponses {
	resps := make(FindCoordinatorResponses)
	if len(names) == 0 {
		return resps
	}

	req := kmsg.NewPtrFindCoordinatorRequest()
	req.CoordinatorType = kind
	req.CoordinatorKeys = names

	keyErr := func(k string, err error) {
		resps[k] = FindCoordinatorResponse{
			Name: k,
			Err:  err,
		}
	}
	allKeysErr := func(req *kmsg.FindCoordinatorRequest, err error) {
		for _, k := range req.CoordinatorKeys {
			keyErr(k, err)
		}
	}

	shards := cl.cl.RequestSharded(ctx, req)
	for _, shard := range shards {
		req := shard.Req.(*kmsg.FindCoordinatorRequest)
		if shard.Err != nil {
			allKeysErr(req, shard.Err)
			continue
		}
		resp := shard.Resp.(*kmsg.FindCoordinatorResponse)
		if err := maybeAuthErr(resp.ErrorCode); err != nil {
			allKeysErr(req, err)
			continue
		}
		for _, c := range resp.Coordinators {
			if err := maybeAuthErr(c.ErrorCode); err != nil {
				keyErr(c.Key, err)
				continue
			}
			resps[c.Key] = FindCoordinatorResponse{ // key is always on one broker, no need to check existence
				Name:       c.Key,
				NodeID:     c.NodeID,
				Host:       c.Host,
				Port:       c.Port,
				Err:        kerr.ErrorForCode(c.ErrorCode),
				ErrMessage: unptrStr(c.ErrorMessage),
			}
		}
	}
	return resps
}

type minmax struct {
	min, max int16
}

// BrokerApiVersions contains the API versions for a single broker.
type BrokerApiVersions struct {
	NodeID int32 // NodeID is the node this API versions response is for.

	raw         *kmsg.ApiVersionsResponse
	keyVersions map[int16]minmax

	Err error // Err is non-nil if the API versions request failed.
}

// Raw returns the raw API versions response.
func (v *BrokerApiVersions) Raw() *kmsg.ApiVersionsResponse {
	return v.raw
}

// KeyVersions returns the broker's min and max version for an API key and
// whether this broker supports the request.
func (v *BrokerApiVersions) KeyVersions(key int16) (min, max int16, exists bool) {
	vs, exists := v.keyVersions[key]
	return vs.min, vs.max, exists
}

// KeyVersions returns the broker's min version for an API key and whether this
// broker supports the request.
func (v *BrokerApiVersions) KeyMinVersion(key int16) (min int16, exists bool) {
	min, _, exists = v.KeyVersions(key)
	return min, exists
}

// KeyVersions returns the broker's max version for an API key and whether this
// broker supports the request.
func (v *BrokerApiVersions) KeyMaxVersion(key int16) (max int16, exists bool) {
	_, max, exists = v.KeyVersions(key)
	return max, exists
}

// EachKeySorted calls fn for every API key in the broker response, from the
// smallest API key to the largest.
func (v *BrokerApiVersions) EachKeySorted(fn func(key, min, max int16)) {
	type kmm struct {
		k, min, max int16
	}
	kmms := make([]kmm, 0, len(v.keyVersions))
	for key, minmax := range v.keyVersions {
		kmms = append(kmms, kmm{key, minmax.min, minmax.max})
	}
	sort.Slice(kmms, func(i, j int) bool { return kmms[i].k < kmms[j].k })
	for _, kmm := range kmms {
		fn(kmm.k, kmm.min, kmm.max)
	}
}

// VersionGuess returns the best guess of Kafka that this broker is. This is a
// shorcut for:
//
//	kversion.FromApiVersionsResponse(v.Raw()).VersionGuess(opt...)
//
// Check the kversion.VersionGuess API docs for more details.
func (v *BrokerApiVersions) VersionGuess(opt ...kversion.VersionGuessOpt) string {
	return kversion.FromApiVersionsResponse(v.raw).VersionGuess(opt...)
}

// BrokerApiVersions contains API versions for all brokers that are reachable
// from a metadata response.
type BrokersApiVersions map[int32]BrokerApiVersions

// Sorted returns all broker responses sorted by node ID.
func (vs BrokersApiVersions) Sorted() []BrokerApiVersions {
	s := make([]BrokerApiVersions, 0, len(vs))
	for _, v := range vs {
		s = append(s, v)
	}
	sort.Slice(s, func(i, j int) bool { return s[i].NodeID < s[j].NodeID })
	return s
}

// Each calls fn for every broker response.
func (vs BrokersApiVersions) Each(fn func(BrokerApiVersions)) {
	for _, v := range vs {
		fn(v)
	}
}

// ApiVersions queries every broker in a metadata response for their API
// versions. This returns an error only if the metadata request fails.
func (cl *Client) ApiVersions(ctx context.Context) (BrokersApiVersions, error) {
	m, err := cl.BrokerMetadata(ctx)
	if err != nil {
		return nil, err
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	vs := make(BrokersApiVersions, len(m.Brokers))
	for _, n := range m.Brokers.NodeIDs() {
		n := n
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := kmsg.NewPtrApiVersionsRequest()
			req.ClientSoftwareName = "kadm"
			req.ClientSoftwareVersion = softwareVersion()
			v := BrokerApiVersions{NodeID: n, keyVersions: make(map[int16]minmax)}
			v.raw, v.Err = req.RequestWith(ctx, cl.cl.Broker(int(n)))

			mu.Lock()
			defer mu.Unlock()
			defer func() { vs[n] = v }()
			if v.Err != nil {
				return
			}

			v.Err = kerr.ErrorForCode(v.raw.ErrorCode)
			for _, k := range v.raw.ApiKeys {
				v.keyVersions[k.ApiKey] = minmax{
					min: k.MinVersion,
					max: k.MaxVersion,
				}
			}
		}()
	}
	wg.Wait()

	return vs, nil
}

// ClientQuotaEntityComponent is a quota entity component.
type ClientQuotaEntityComponent struct {
	Type string  // Type is the entity type ("user", "client-id", "ip").
	Name *string // Name is the entity name, or null if the default.
}

// String returns key=value, or key=<default> if value is nil.
func (d ClientQuotaEntityComponent) String() string {
	if d.Name == nil {
		return d.Type + "=<default>"
	}
	return fmt.Sprintf("%s=%s", d.Type, *d.Name)
}

// ClientQuotaEntity contains the components that make up a single entity.
type ClientQuotaEntity []ClientQuotaEntityComponent

// String returns {key=value, key=value}, joining all entities with a ", " and
// wrapping in braces.
func (ds ClientQuotaEntity) String() string {
	var ss []string
	for _, d := range ds {
		ss = append(ss, d.String())
	}
	return "{" + strings.Join(ss, ", ") + "}"
}

// ClientQuotaValue is a quota name and value.
type ClientQuotaValue struct {
	Key   string  // Key is the quota configuration key.
	Value float64 // Value is the quota configuration value.
}

// String returns key=value.
func (d ClientQuotaValue) String() string {
	return fmt.Sprintf("%s=%f", d.Key, d.Value)
}

// ClientQuotaValues contains all client quota values.
type ClientQuotaValues []ClientQuotaValue

// QuotasMatchType specifies how to match a described client quota entity.
//
// 0 means to match the name exactly: user=foo will only match components of
// entity type "user" and entity name "foo".
//
// 1 means to match the default of the name: entity type "user" with a default
// match will return the default quotas for user entities.
//
// 2 means to match any name: entity type "user" with any matching will return
// both names and defaults.
type QuotasMatchType = kmsg.QuotasMatchType

// DescribeClientQuotaComponent is an input entity component to describing
// client quotas: we define the type of quota ("client-id", "user"), how to
// match, and the match name if needed.
type DescribeClientQuotaComponent struct {
	Type      string          // Type is the type of entity component to describe ("user", "client-id", "ip").
	MatchName *string         // MatchName is the name to match again; this is only needed when MatchType is 0 (exact).
	MatchType QuotasMatchType // MatchType is how to match an entity.
}

// DescribedClientQuota contains a described quota. A single quota is made up
// of multiple entities and multiple values, for example, "user=foo" is one
// component of the entity, and "client-id=bar" is another.
type DescribedClientQuota struct {
	Entity ClientQuotaEntity // Entity is the entity of this described client quota.
	Values ClientQuotaValues // Values contains the quota valies for this entity.
}

// DescribedClientQuota contains client quotas that were described.
type DescribedClientQuotas []DescribedClientQuota

// DescribeClientQuotas describes client quotas. If strict is true, the
// response includes only the requested components.
func (cl *Client) DescribeClientQuotas(ctx context.Context, strict bool, entityComponents []DescribeClientQuotaComponent) (DescribedClientQuotas, error) {
	req := kmsg.NewPtrDescribeClientQuotasRequest()
	req.Strict = strict
	for _, entity := range entityComponents {
		rc := kmsg.NewDescribeClientQuotasRequestComponent()
		rc.EntityType = entity.Type
		rc.Match = entity.MatchName
		rc.MatchType = entity.MatchType
		req.Components = append(req.Components, rc)
	}
	resp, err := req.RequestWith(ctx, cl.cl)
	if err != nil {
		return nil, err
	}
	if err := maybeAuthErr(resp.ErrorCode); err != nil {
		return nil, err
	}
	if err := kerr.ErrorForCode(resp.ErrorCode); err != nil {
		return nil, &ErrAndMessage{err, unptrStr(resp.ErrorMessage)}
	}
	var qs DescribedClientQuotas
	for _, entry := range resp.Entries {
		var q DescribedClientQuota
		for _, e := range entry.Entity {
			q.Entity = append(q.Entity, ClientQuotaEntityComponent{
				Type: e.Type,
				Name: e.Name,
			})
		}
		for _, v := range entry.Values {
			q.Values = append(q.Values, ClientQuotaValue{
				Key:   v.Key,
				Value: v.Value,
			})
		}
		qs = append(qs, q)
	}
	return qs, nil
}

// AlterClientQuotaOp sets or remove a client quota.
type AlterClientQuotaOp struct {
	Key    string  // Key is the quota configuration key to set or remove.
	Value  float64 // Value is the quota configuration value to set or remove.
	Remove bool    // Remove, if true, removes this quota rather than sets it.
}

// AlterClientQuotaEntry pairs an entity with quotas to set or remove.
type AlterClientQuotaEntry struct {
	Entity ClientQuotaEntity    // Entity is the entity to alter quotas for.
	Ops    []AlterClientQuotaOp // Ops are quotas to set or remove.
}

// AlteredClientQuota is the result for a single entity that was altered.
type AlteredClientQuota struct {
	Entity     ClientQuotaEntity // Entity is the entity this result is for.
	Err        error             // Err is non-nil if the alter operation on this entity failed.
	ErrMessage string            // ErrMessage is an optional additional message on error.
}

// AlteredClientQuotas contains results for all altered entities.
type AlteredClientQuotas []AlteredClientQuota

// AlterClientQuotas alters quotas for the input entries. You may consider
// checking ValidateAlterClientQuotas before using this method.
func (cl *Client) AlterClientQuotas(ctx context.Context, entries []AlterClientQuotaEntry) (AlteredClientQuotas, error) {
	return cl.alterClientQuotas(ctx, false, entries)
}

// ValidateAlterClientQuotas validates an alter client quota request. This
// returns exactly what AlterClientQuotas returns, but does not actually alter
// quotas.
func (cl *Client) ValidateAlterClientQuotas(ctx context.Context, entries []AlterClientQuotaEntry) (AlteredClientQuotas, error) {
	return cl.alterClientQuotas(ctx, true, entries)
}

func (cl *Client) alterClientQuotas(ctx context.Context, validate bool, entries []AlterClientQuotaEntry) (AlteredClientQuotas, error) {
	req := kmsg.NewPtrAlterClientQuotasRequest()
	req.ValidateOnly = validate
	for _, entry := range entries {
		re := kmsg.NewAlterClientQuotasRequestEntry()
		for _, c := range entry.Entity {
			rec := kmsg.NewAlterClientQuotasRequestEntryEntity()
			rec.Type = c.Type
			rec.Name = c.Name
			re.Entity = append(re.Entity, rec)
		}
		for _, op := range entry.Ops {
			reo := kmsg.NewAlterClientQuotasRequestEntryOp()
			reo.Key = op.Key
			reo.Value = op.Value
			reo.Remove = op.Remove
			re.Ops = append(re.Ops, reo)
		}
		req.Entries = append(req.Entries, re)
	}
	resp, err := req.RequestWith(ctx, cl.cl)
	if err != nil {
		return nil, err
	}
	var as AlteredClientQuotas
	for _, entry := range resp.Entries {
		var e ClientQuotaEntity
		for _, c := range entry.Entity {
			e = append(e, ClientQuotaEntityComponent{
				Type: c.Type,
				Name: c.Name,
			})
		}
		a := AlteredClientQuota{
			Entity:     e,
			Err:        kerr.ErrorForCode(entry.ErrorCode),
			ErrMessage: unptrStr(entry.ErrorMessage),
		}
		as = append(as, a)
	}
	return as, nil
}

// ScramMechanism is a SCRAM mechanism.
type ScramMechanism int8

const (
	// ScramSha256 represents the SCRAM-SHA-256 mechanism.
	ScramSha256 ScramMechanism = 1
	// ScramSha512 represents the SCRAM-SHA-512 mechanism.
	ScramSha512 ScramMechanism = 2
)

// String returns either SCRAM-SHA-256, SCRAM-SHA-512, or UNKNOWN.
func (s ScramMechanism) String() string {
	switch s {
	case ScramSha256:
		return "SCRAM-SHA-256"
	case ScramSha512:
		return "SCRAM-SHA-512"
	default:
		return "UNKNOWN"
	}
}

// CredInfo contains the SCRAM mechanism and iterations for a password.
type CredInfo struct {
	// Mechanism is the SCRAM mechanism a password exists for. This is 0
	// for UNKNOWN, 1 for SCRAM-SHA-256, and 2 for SCRAM-SHA-512.
	Mechanism ScramMechanism
	// Iterations is the number of SCRAM iterations for this password.
	Iterations int32
}

// String returns MECHANISM=iterations={c.Iterations}.
func (c CredInfo) String() string {
	return fmt.Sprintf("%s=iterations=%d", c.Mechanism, c.Iterations)
}

// DescribedUserSCRAM contains a user, the SCRAM mechanisms that the user has
// passwords for, and if describing the user SCRAM credentials errored.
type DescribedUserSCRAM struct {
	User       string     // User is the user this described user credential is for.
	CredInfos  []CredInfo // CredInfos contains SCRAM mechanisms the user has passwords for.
	Err        error      // Err is any error encountered when describing the user.
	ErrMessage string     // ErrMessage a potential extra message describing any error.
}

// DescribedUserSCRAMs contains described user SCRAM credentials keyed by user.
type DescribedUserSCRAMs map[string]DescribedUserSCRAM

// Sorted returns the described user credentials ordered by user.
func (ds DescribedUserSCRAMs) Sorted() []DescribedUserSCRAM {
	s := make([]DescribedUserSCRAM, 0, len(ds))
	for _, d := range ds {
		s = append(s, d)
	}
	sort.Slice(s, func(i, j int) bool { return s[i].User < s[j].User })
	return s
}

// AllFailed returns whether all described user credentials are errored.
func (ds DescribedUserSCRAMs) AllFailed() bool {
	var n int
	ds.EachError(func(DescribedUserSCRAM) { n++ })
	return len(ds) > 0 && n == len(ds)
}

// EachError calls fn for every described user that has a non-nil error.
func (ds DescribedUserSCRAMs) EachError(fn func(DescribedUserSCRAM)) {
	for _, d := range ds {
		if d.Err != nil {
			fn(d)
		}
	}
}

// Each calls fn for every described user.
func (ds DescribedUserSCRAMs) Each(fn func(DescribedUserSCRAM)) {
	for _, d := range ds {
		fn(d)
	}
}

// Error iterates over all described users and returns the first error
// encountered, if any.
func (ds DescribedUserSCRAMs) Error() error {
	for _, d := range ds {
		if d.Err != nil {
			return d.Err
		}
	}
	return nil
}

// Ok returns true if there are no errors. This is a shortcut for rs.Error() ==
// nil.
func (ds DescribedUserSCRAMs) Ok() bool {
	return ds.Error() == nil
}

// DescribeUserSCRAMs returns a small bit of information about all users in the
// input request that have SCRAM passwords configured.  No users requests all
// users.
func (cl *Client) DescribeUserSCRAMs(ctx context.Context, users ...string) (DescribedUserSCRAMs, error) {
	req := kmsg.NewPtrDescribeUserSCRAMCredentialsRequest()
	for _, u := range users {
		ru := kmsg.NewDescribeUserSCRAMCredentialsRequestUser()
		ru.Name = u
		req.Users = append(req.Users, ru)
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
	rs := make(DescribedUserSCRAMs)
	for _, res := range resp.Results {
		r := DescribedUserSCRAM{
			User:       res.User,
			Err:        kerr.ErrorForCode(res.ErrorCode),
			ErrMessage: unptrStr(res.ErrorMessage),
		}
		for _, i := range res.CredentialInfos {
			r.CredInfos = append(r.CredInfos, CredInfo{
				Mechanism:  ScramMechanism(i.Mechanism),
				Iterations: i.Iterations,
			})
		}
		rs[r.User] = r
	}
	return rs, nil
}

// DeleteSCRAM deletes a password with the given mechanism for the user.
type DeleteSCRAM struct {
	User      string         // User is the username to match for deletion.
	Mechanism ScramMechanism // Mechanism is the mechanism to match to delete a password for.
}

// UpsertSCRAM either updates or creates (inserts) a new password for a user.
// There are two ways to specify a password: either with the Password field
// directly, or by specifying both Salt and SaltedPassword. If you specify just
// a password, this package generates a 24 byte salt and uses pbkdf2 to create
// the salted password.
type UpsertSCRAM struct {
	User           string         // User is the username to use.
	Mechanism      ScramMechanism // Mechanism is the mechanism to use.
	Iterations     int32          // Iterations is the SCRAM iterations to use; must be between 4096 and 16384.
	Password       string         // Password is the password to salt and convert to a salted password. Requires Salt and SaltedPassword to be empty.
	Salt           []byte         // Salt must be paired with SaltedPassword and requires Password to be empty.
	SaltedPassword []byte         // SaltedPassword must be paired with Salt and requires Password to be empty.
}

// AlteredUserSCRAM is the result of an alter operation.
type AlteredUserSCRAM struct {
	User       string // User is the username that was altered.
	Err        error  // Err is any error encountered when altering the user.
	ErrMessage string // ErrMessage a potential extra message describing any error.
}

// AlteredUserSCRAMs contains altered user SCRAM credentials keyed by user.
type AlteredUserSCRAMs map[string]AlteredUserSCRAM

// Sorted returns the altered user credentials ordered by user.
func (as AlteredUserSCRAMs) Sorted() []AlteredUserSCRAM {
	s := make([]AlteredUserSCRAM, 0, len(as))
	for _, a := range as {
		s = append(s, a)
	}
	sort.Slice(s, func(i, j int) bool { return s[i].User < s[j].User })
	return s
}

// AllFailed returns whether all altered user credentials are errored.
func (as AlteredUserSCRAMs) AllFailed() bool {
	var n int
	as.EachError(func(AlteredUserSCRAM) { n++ })
	return len(as) > 0 && n == len(as)
}

// EachError calls fn for every altered user that has a non-nil error.
func (as AlteredUserSCRAMs) EachError(fn func(AlteredUserSCRAM)) {
	for _, a := range as {
		if a.Err != nil {
			fn(a)
		}
	}
}

// Each calls fn for every altered user.
func (as AlteredUserSCRAMs) Each(fn func(AlteredUserSCRAM)) {
	for _, a := range as {
		fn(a)
	}
}

// Error iterates over all altered users and returns the first error
// encountered, if any.
func (as AlteredUserSCRAMs) Error() error {
	for _, a := range as {
		if a.Err != nil {
			return a.Err
		}
	}
	return nil
}

// Ok returns true if there are no errors. This is a shortcut for rs.Error() ==
// nil.
func (as AlteredUserSCRAMs) Ok() bool {
	return as.Error() == nil
}

// AlterUserSCRAMs deletes, updates, or creates (inserts) user SCRAM
// credentials. Note that a username can only appear once across both upserts
// and deletes. This modifies elements of the upsert slice that need to have a
// salted password generated.
func (cl *Client) AlterUserSCRAMs(ctx context.Context, del []DeleteSCRAM, upsert []UpsertSCRAM) (AlteredUserSCRAMs, error) {
	for i, u := range upsert {
		if u.Password != "" {
			if len(u.Salt) > 0 || len(u.SaltedPassword) > 0 {
				return nil, fmt.Errorf("user %s: cannot specify both a password and a salt / salted password", u.User)
			}
			u.Salt = make([]byte, 24)
			if _, err := rand.Read(u.Salt); err != nil {
				return nil, fmt.Errorf("user %s: unable to generate salt: %v", u.User, err)
			}
			switch u.Mechanism {
			case ScramSha256:
				u.SaltedPassword = pbkdf2.Key([]byte(u.Password), u.Salt, int(u.Iterations), sha256.Size, sha256.New)
			case ScramSha512:
				u.SaltedPassword = pbkdf2.Key([]byte(u.Password), u.Salt, int(u.Iterations), sha512.Size, sha512.New)
			default:
				return nil, fmt.Errorf("user %s: unknown mechanism, unable to generate password", u.User)
			}
			upsert[i] = u
		} else {
			if len(u.Salt) == 0 || len(u.SaltedPassword) == 0 {
				return nil, fmt.Errorf("user %s: must specify either a password or a salt and salted password", u.User)
			}
		}
	}

	req := kmsg.NewPtrAlterUserSCRAMCredentialsRequest()
	for _, d := range del {
		rd := kmsg.NewAlterUserSCRAMCredentialsRequestDeletion()
		rd.Name = d.User
		rd.Mechanism = int8(d.Mechanism)
		req.Deletions = append(req.Deletions, rd)
	}
	for _, u := range upsert {
		ru := kmsg.NewAlterUserSCRAMCredentialsRequestUpsertion()
		ru.Name = u.User
		ru.Mechanism = int8(u.Mechanism)
		ru.Iterations = u.Iterations
		ru.Salt = u.Salt
		ru.SaltedPassword = u.SaltedPassword
		req.Upsertions = append(req.Upsertions, ru)
	}
	resp, err := req.RequestWith(ctx, cl.cl)
	if err != nil {
		return nil, err
	}
	rs := make(AlteredUserSCRAMs)
	for _, res := range resp.Results {
		if err := maybeAuthErr(res.ErrorCode); err != nil {
			return nil, err
		}
		r := AlteredUserSCRAM{
			User:       res.User,
			Err:        kerr.ErrorForCode(res.ErrorCode),
			ErrMessage: unptrStr(res.ErrorMessage),
		}
		rs[r.User] = r
	}
	return rs, nil
}

// ElectLeadersHow is how partition leaders should be elected.
type ElectLeadersHow int8

const (
	// ElectPreferredReplica elects the preferred replica for a partition.
	ElectPreferredReplica ElectLeadersHow = 0
	// ElectLiveReplica elects the first life replica if there are no
	// in-sync replicas (i.e., this is unclean leader election).
	ElectLiveReplica ElectLeadersHow = 1
)

// ElectLeadersResult is the result for a single partition in an elect leaders
// request.
type ElectLeadersResult struct {
	Topic      string          // Topic is the topic this result is for.
	Partition  int32           // Partition is the partition this result is for.
	How        ElectLeadersHow // How is the type of election that was performed.
	Err        error           // Err is non-nil if electing this partition's leader failed, such as the partition not existing or the preferred leader is not available and you used ElectPreferredReplica.
	ErrMessage string          // ErrMessage a potential extra message describing any error.
}

// ElectLeadersResults contains per-topic, per-partition results for an elect
// leaders request.
type ElectLeadersResults map[string]map[int32]ElectLeadersResult

// ElectLeaders elects leaders for partitions. This request was added in Kafka
// 2.2 to replace the previously-ZooKeeper-only option of triggering leader
// elections. See KIP-183 for more details.
//
// Kafka 2.4 introduced the ability to use unclean leader election. If you use
// unclean leader election on a Kafka 2.2 or 2.3 cluster, the client will
// instead fall back to preferred replica (clean) leader election. You can
// check the result's How function (or field) to see.
//
// If s is nil, this will elect leaders for all partitions.
//
// This will return *AuthError if you do not have ALTER on CLUSTER for
// kafka-cluster.
func (cl *Client) ElectLeaders(ctx context.Context, how ElectLeadersHow, s TopicsSet) (ElectLeadersResults, error) {
	req := kmsg.NewPtrElectLeadersRequest()
	req.ElectionType = int8(how)
	for _, t := range s.IntoList() {
		rt := kmsg.NewElectLeadersRequestTopic()
		rt.Topic = t.Topic
		rt.Partitions = t.Partitions
		req.Topics = append(req.Topics, rt)
	}
	resp, err := req.RequestWith(ctx, cl.cl)
	if err != nil {
		return nil, err
	}
	if err := maybeAuthErr(resp.ErrorCode); err != nil {
		return nil, err
	}
	if resp.Version == 0 { // v0 does not have the election type field
		how = ElectPreferredReplica
	}
	rs := make(ElectLeadersResults)
	for _, t := range resp.Topics {
		rt := make(map[int32]ElectLeadersResult)
		rs[t.Topic] = rt
		for _, p := range t.Partitions {
			if err := maybeAuthErr(p.ErrorCode); err != nil {
				return nil, err // v0 has no top-level err
			}
			rt[p.Partition] = ElectLeadersResult{
				Topic:      t.Topic,
				Partition:  p.Partition,
				How:        how,
				Err:        kerr.ErrorForCode(p.ErrorCode),
				ErrMessage: unptrStr(p.ErrorMessage),
			}
		}
	}
	return rs, nil
}

// OffsetForLeaderEpochRequest contains topics, partitions, and leader epochs
// to request offsets for in an OffsetForLeaderEpoch.
type OffsetForLeaderEpochRequest map[string]map[int32]int32

// Add adds a topic, partition, and leader epoch to the request.
func (l *OffsetForLeaderEpochRequest) Add(topic string, partition, leaderEpoch int32) {
	if *l == nil {
		*l = make(map[string]map[int32]int32)
	}
	t := (*l)[topic]
	if t == nil {
		t = make(map[int32]int32)
		(*l)[topic] = t
	}
	t[partition] = leaderEpoch
}

// OffsetForLeaderEpoch contains a response for a single partition in an
// OffsetForLeaderEpoch request.
type OffsetForLeaderEpoch struct {
	NodeID    int32  // NodeID is the node that is the leader of this topic / partition.
	Topic     string // Topic is the topic this leader epoch response is for.
	Partition int32  // Partition is the partition this leader epoch response is for.

	// LeaderEpoch is either
	//
	// 1) -1, if the requested LeaderEpoch is unknown.
	//
	// 2) Less than the requested LeaderEpoch, if the requested LeaderEpoch
	// exists but has no records in it. For example, epoch 1 had end offset
	// 37, then epoch 2 and 3 had no records: if you request LeaderEpoch 3,
	// this will return LeaderEpoch 1 with EndOffset 37.
	//
	// 3) Equal to the requested LeaderEpoch, if the requested LeaderEpoch
	// is equal to or less than the current epoch for the partition.
	LeaderEpoch int32

	// EndOffset is either
	//
	// 1) The LogEndOffset, if the broker has the same LeaderEpoch as the
	// request.
	//
	// 2) the beginning offset of the next LeaderEpoch, if the broker has a
	// higher LeaderEpoch.
	//
	// The second option allows the user to detect data loss: if the
	// consumer consumed past the EndOffset that is returned, then the
	// consumer should reset to the returned offset and the consumer knows
	// that everything from the returned offset to the requested offset was
	// lost.
	EndOffset int64

	// Err is non-nil if this partition had a response error.
	Err error
}

// OffsetsForLeaderEpochs contains responses for partitions in a
// OffsetForLeaderEpochRequest.
type OffsetsForLeaderEpochs map[string]map[int32]OffsetForLeaderEpoch

// OffsetForLeaderEpoch requests end offsets for the requested leader epoch in
// partitions in the request. This is a relatively advanced and client internal
// request, for more details, see the doc comments on the OffsetForLeaderEpoch
// type.
//
// This may return *ShardErrors or *AuthError.
func (cl *Client) OffetForLeaderEpoch(ctx context.Context, r OffsetForLeaderEpochRequest) (OffsetsForLeaderEpochs, error) {
	req := kmsg.NewPtrOffsetForLeaderEpochRequest()
	for t, ps := range r {
		rt := kmsg.NewOffsetForLeaderEpochRequestTopic()
		rt.Topic = t
		for p, e := range ps {
			rp := kmsg.NewOffsetForLeaderEpochRequestTopicPartition()
			rp.Partition = p
			rp.LeaderEpoch = e
			rt.Partitions = append(rt.Partitions, rp)
		}
		req.Topics = append(req.Topics, rt)
	}
	shards := cl.cl.RequestSharded(ctx, req)
	ls := make(OffsetsForLeaderEpochs)
	return ls, shardErrEachBroker(req, shards, func(b BrokerDetail, kr kmsg.Response) error {
		resp := kr.(*kmsg.OffsetForLeaderEpochResponse)
		for _, rt := range resp.Topics {
			lps, exists := ls[rt.Topic]
			if !exists { // topic partitions could be spread around brokers, need to check existence
				lps = make(map[int32]OffsetForLeaderEpoch)
				ls[rt.Topic] = lps
			}
			for _, rp := range rt.Partitions {
				if err := maybeAuthErr(rp.ErrorCode); err != nil {
					return err
				}
				lps[rp.Partition] = OffsetForLeaderEpoch{ // one partition globally, no need to exist check
					NodeID:      b.NodeID,
					Topic:       rt.Topic,
					Partition:   rp.Partition,
					LeaderEpoch: rp.LeaderEpoch,
					EndOffset:   rp.EndOffset,
					Err:         kerr.ErrorForCode(rp.ErrorCode),
				}
			}
		}
		return nil
	})
}
