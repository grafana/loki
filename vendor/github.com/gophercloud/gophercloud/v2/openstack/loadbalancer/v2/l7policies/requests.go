package l7policies

import (
	"context"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/pagination"
)

// CreateOptsBuilder allows extensions to add additional parameters to the
// Create request.
type CreateOptsBuilder interface {
	ToL7PolicyCreateMap() (map[string]any, error)
}

type Action string
type RuleType string
type CompareType string

const (
	ActionRedirectPrefix Action = "REDIRECT_PREFIX"
	ActionRedirectToPool Action = "REDIRECT_TO_POOL"
	ActionRedirectToURL  Action = "REDIRECT_TO_URL"
	ActionReject         Action = "REJECT"

	TypeCookie          RuleType = "COOKIE"
	TypeFileType        RuleType = "FILE_TYPE"
	TypeHeader          RuleType = "HEADER"
	TypeHostName        RuleType = "HOST_NAME"
	TypePath            RuleType = "PATH"
	TypeSSLConnHasCert  RuleType = "SSL_CONN_HAS_CERT"
	TypeSSLVerifyResult RuleType = "SSL_VERIFY_RESULT"
	TypeSSLDNField      RuleType = "SSL_DN_FIELD"

	CompareTypeContains  CompareType = "CONTAINS"
	CompareTypeEndWith   CompareType = "ENDS_WITH"
	CompareTypeEqual     CompareType = "EQUAL_TO"
	CompareTypeRegex     CompareType = "REGEX"
	CompareTypeStartWith CompareType = "STARTS_WITH"
)

// CreateOpts is the common options struct used in this package's Create
// operation.
type CreateOpts struct {
	// Name of the L7 policy.
	Name string `json:"name,omitempty"`

	// The ID of the listener.
	ListenerID string `json:"listener_id,omitempty"`

	// The L7 policy action. One of REDIRECT_PREFIX, REDIRECT_TO_POOL, REDIRECT_TO_URL, or REJECT.
	Action Action `json:"action" required:"true"`

	// The position of this policy on the listener.
	Position int32 `json:"position,omitempty"`

	// A human-readable description for the resource.
	Description string `json:"description,omitempty"`

	// ProjectID is the UUID of the project who owns the L7 policy in octavia.
	// Only administrative users can specify a project UUID other than their own.
	ProjectID string `json:"project_id,omitempty"`

	// Requests matching this policy will be redirected to this Prefix URL.
	// Only valid if action is REDIRECT_PREFIX.
	RedirectPrefix string `json:"redirect_prefix,omitempty"`

	// Requests matching this policy will be redirected to the pool with this ID.
	// Only valid if action is REDIRECT_TO_POOL.
	RedirectPoolID string `json:"redirect_pool_id,omitempty"`

	// Requests matching this policy will be redirected to this URL.
	// Only valid if action is REDIRECT_TO_URL.
	RedirectURL string `json:"redirect_url,omitempty"`

	// Requests matching this policy will be redirected to the specified URL or Prefix URL
	// with the HTTP response code. Valid if action is REDIRECT_TO_URL or REDIRECT_PREFIX.
	// Valid options are: 301, 302, 303, 307, or 308. Default is 302. Requires version 2.9
	RedirectHttpCode int32 `json:"redirect_http_code,omitempty"`

	// The administrative state of the Loadbalancer. A valid value is true (UP)
	// or false (DOWN).
	AdminStateUp *bool `json:"admin_state_up,omitempty"`

	// Rules is a slice of CreateRuleOpts which allows a set of rules
	// to be created at the same time the policy is created.
	//
	// This is only possible to use when creating a fully populated
	// Loadbalancer.
	Rules []CreateRuleOpts `json:"rules,omitempty"`

	// Tags is a set of resource tags. Requires version 2.5.
	Tags []string `json:"tags,omitempty"`
}

// ToL7PolicyCreateMap builds a request body from CreateOpts.
func (opts CreateOpts) ToL7PolicyCreateMap() (map[string]any, error) {
	return gophercloud.BuildRequestBody(opts, "l7policy")
}

// Create accepts a CreateOpts struct and uses the values to create a new l7policy.
func Create(ctx context.Context, c *gophercloud.ServiceClient, opts CreateOptsBuilder) (r CreateResult) {
	b, err := opts.ToL7PolicyCreateMap()
	if err != nil {
		r.Err = err
		return
	}
	resp, err := c.Post(ctx, rootURL(c), b, &r.Body, nil)
	_, r.Header, r.Err = gophercloud.ParseResponse(resp, err)
	return
}

// ListOptsBuilder allows extensions to add additional parameters to the
// List request.
type ListOptsBuilder interface {
	ToL7PolicyListQuery() (string, error)
}

// ListOpts allows the filtering and sorting of paginated collections through
// the API.
type ListOpts struct {
	Name           string `q:"name"`
	Description    string `q:"description"`
	ListenerID     string `q:"listener_id"`
	Action         string `q:"action"`
	ProjectID      string `q:"project_id"`
	RedirectPoolID string `q:"redirect_pool_id"`
	RedirectURL    string `q:"redirect_url"`
	Position       int32  `q:"position"`
	AdminStateUp   bool   `q:"admin_state_up"`
	ID             string `q:"id"`
	Limit          int    `q:"limit"`
	Marker         string `q:"marker"`
	SortKey        string `q:"sort_key"`
	SortDir        string `q:"sort_dir"`
}

// ToL7PolicyListQuery formats a ListOpts into a query string.
func (opts ListOpts) ToL7PolicyListQuery() (string, error) {
	q, err := gophercloud.BuildQueryString(opts)
	return q.String(), err
}

// List returns a Pager which allows you to iterate over a collection of
// l7policies. It accepts a ListOpts struct, which allows you to filter and sort
// the returned collection for greater efficiency.
//
// Default policy settings return only those l7policies that are owned by the
// project who submits the request, unless an admin user submits the request.
func List(c *gophercloud.ServiceClient, opts ListOptsBuilder) pagination.Pager {
	url := rootURL(c)
	if opts != nil {
		query, err := opts.ToL7PolicyListQuery()
		if err != nil {
			return pagination.Pager{Err: err}
		}
		url += query
	}
	return pagination.NewPager(c, url, func(r pagination.PageResult) pagination.Page {
		return L7PolicyPage{pagination.LinkedPageBase{PageResult: r}}
	})
}

// Get retrieves a particular l7policy based on its unique ID.
func Get(ctx context.Context, c *gophercloud.ServiceClient, id string) (r GetResult) {
	resp, err := c.Get(ctx, resourceURL(c, id), &r.Body, nil)
	_, r.Header, r.Err = gophercloud.ParseResponse(resp, err)
	return
}

// Delete will permanently delete a particular l7policy based on its unique ID.
func Delete(ctx context.Context, c *gophercloud.ServiceClient, id string) (r DeleteResult) {
	resp, err := c.Delete(ctx, resourceURL(c, id), nil)
	_, r.Header, r.Err = gophercloud.ParseResponse(resp, err)
	return
}

// UpdateOptsBuilder allows extensions to add additional parameters to the
// Update request.
type UpdateOptsBuilder interface {
	ToL7PolicyUpdateMap() (map[string]any, error)
}

// UpdateOpts is the common options struct used in this package's Update
// operation.
type UpdateOpts struct {
	// Name of the L7 policy, empty string is allowed.
	Name *string `json:"name,omitempty"`

	// The L7 policy action. One of REDIRECT_PREFIX, REDIRECT_TO_POOL, REDIRECT_TO_URL, or REJECT.
	Action Action `json:"action,omitempty"`

	// The position of this policy on the listener.
	Position int32 `json:"position,omitempty"`

	// A human-readable description for the resource, empty string is allowed.
	Description *string `json:"description,omitempty"`

	// Requests matching this policy will be redirected to this Prefix URL.
	// Only valid if action is REDIRECT_PREFIX.
	RedirectPrefix *string `json:"redirect_prefix,omitempty"`

	// Requests matching this policy will be redirected to the pool with this ID.
	// Only valid if action is REDIRECT_TO_POOL.
	RedirectPoolID *string `json:"redirect_pool_id,omitempty"`

	// Requests matching this policy will be redirected to this URL.
	// Only valid if action is REDIRECT_TO_URL.
	RedirectURL *string `json:"redirect_url,omitempty"`

	// Requests matching this policy will be redirected to the specified URL or Prefix URL
	// with the HTTP response code. Valid if action is REDIRECT_TO_URL or REDIRECT_PREFIX.
	// Valid options are: 301, 302, 303, 307, or 308. Default is 302. Requires version 2.9
	RedirectHttpCode int32 `json:"redirect_http_code,omitempty"`

	// The administrative state of the Loadbalancer. A valid value is true (UP)
	// or false (DOWN).
	AdminStateUp *bool `json:"admin_state_up,omitempty"`

	// Tags is a set of resource tags. Requires version 2.5.
	Tags *[]string `json:"tags,omitempty"`
}

// ToL7PolicyUpdateMap builds a request body from UpdateOpts.
func (opts UpdateOpts) ToL7PolicyUpdateMap() (map[string]any, error) {
	b, err := gophercloud.BuildRequestBody(opts, "l7policy")
	if err != nil {
		return nil, err
	}

	m := b["l7policy"].(map[string]any)

	if m["redirect_pool_id"] == "" {
		m["redirect_pool_id"] = nil
	}

	if m["redirect_url"] == "" {
		m["redirect_url"] = nil
	}

	if m["redirect_prefix"] == "" {
		m["redirect_prefix"] = nil
	}

	if m["redirect_http_code"] == 0 {
		m["redirect_http_code"] = nil
	}

	return b, nil
}

// Update allows l7policy to be updated.
func Update(ctx context.Context, c *gophercloud.ServiceClient, id string, opts UpdateOptsBuilder) (r UpdateResult) {
	b, err := opts.ToL7PolicyUpdateMap()
	if err != nil {
		r.Err = err
		return
	}
	resp, err := c.Put(ctx, resourceURL(c, id), b, &r.Body, &gophercloud.RequestOpts{
		OkCodes: []int{200},
	})
	_, r.Header, r.Err = gophercloud.ParseResponse(resp, err)
	return
}

// CreateRuleOpts is the common options struct used in this package's CreateRule
// operation.
type CreateRuleOpts struct {
	// The L7 rule type. One of COOKIE, FILE_TYPE, HEADER, HOST_NAME, or PATH.
	RuleType RuleType `json:"type" required:"true"`

	// The comparison type for the L7 rule. One of CONTAINS, ENDS_WITH, EQUAL_TO, REGEX, or STARTS_WITH.
	CompareType CompareType `json:"compare_type" required:"true"`

	// The value to use for the comparison. For example, the file type to compare.
	Value string `json:"value" required:"true"`

	// ProjectID is the UUID of the project who owns the rule in octavia.
	// Only administrative users can specify a project UUID other than their own.
	ProjectID string `json:"project_id,omitempty"`

	// The key to use for the comparison. For example, the name of the cookie to evaluate.
	Key string `json:"key,omitempty"`

	// When true the logic of the rule is inverted. For example, with invert true,
	// equal to would become not equal to. Default is false.
	Invert bool `json:"invert,omitempty"`

	// The administrative state of the Loadbalancer. A valid value is true (UP)
	// or false (DOWN).
	AdminStateUp *bool `json:"admin_state_up,omitempty"`

	// Tags is a set of resource tags. Requires version 2.5.
	Tags []string `json:"tags,omitempty"`
}

// ToRuleCreateMap builds a request body from CreateRuleOpts.
func (opts CreateRuleOpts) ToRuleCreateMap() (map[string]any, error) {
	return gophercloud.BuildRequestBody(opts, "rule")
}

// CreateRule will create and associate a Rule with a particular L7Policy.
func CreateRule(ctx context.Context, c *gophercloud.ServiceClient, policyID string, opts CreateRuleOpts) (r CreateRuleResult) {
	b, err := opts.ToRuleCreateMap()
	if err != nil {
		r.Err = err
		return
	}
	resp, err := c.Post(ctx, ruleRootURL(c, policyID), b, &r.Body, nil)
	_, r.Header, r.Err = gophercloud.ParseResponse(resp, err)
	return
}

// ListRulesOptsBuilder allows extensions to add additional parameters to the
// ListRules request.
type ListRulesOptsBuilder interface {
	ToRulesListQuery() (string, error)
}

// ListRulesOpts allows the filtering and sorting of paginated collections
// through the API.
type ListRulesOpts struct {
	RuleType     RuleType    `q:"type"`
	ProjectID    string      `q:"project_id"`
	CompareType  CompareType `q:"compare_type"`
	Value        string      `q:"value"`
	Key          string      `q:"key"`
	Invert       bool        `q:"invert"`
	AdminStateUp bool        `q:"admin_state_up"`
	ID           string      `q:"id"`
	Limit        int         `q:"limit"`
	Marker       string      `q:"marker"`
	SortKey      string      `q:"sort_key"`
	SortDir      string      `q:"sort_dir"`
}

// ToRulesListQuery formats a ListOpts into a query string.
func (opts ListRulesOpts) ToRulesListQuery() (string, error) {
	q, err := gophercloud.BuildQueryString(opts)
	return q.String(), err
}

// ListRules returns a Pager which allows you to iterate over a collection of
// rules. It accepts a ListRulesOptsBuilder, which allows you to filter and
// sort the returned collection for greater efficiency.
//
// Default policy settings return only those rules that are owned by the
// project who submits the request, unless an admin user submits the request.
func ListRules(c *gophercloud.ServiceClient, policyID string, opts ListRulesOptsBuilder) pagination.Pager {
	url := ruleRootURL(c, policyID)
	if opts != nil {
		query, err := opts.ToRulesListQuery()
		if err != nil {
			return pagination.Pager{Err: err}
		}
		url += query
	}
	return pagination.NewPager(c, url, func(r pagination.PageResult) pagination.Page {
		return RulePage{pagination.LinkedPageBase{PageResult: r}}
	})
}

// GetRule retrieves a particular L7Policy Rule based on its unique ID.
func GetRule(ctx context.Context, c *gophercloud.ServiceClient, policyID string, ruleID string) (r GetRuleResult) {
	resp, err := c.Get(ctx, ruleResourceURL(c, policyID, ruleID), &r.Body, nil)
	_, r.Header, r.Err = gophercloud.ParseResponse(resp, err)
	return
}

// DeleteRule will remove a Rule from a particular L7Policy.
func DeleteRule(ctx context.Context, c *gophercloud.ServiceClient, policyID string, ruleID string) (r DeleteRuleResult) {
	resp, err := c.Delete(ctx, ruleResourceURL(c, policyID, ruleID), nil)
	_, r.Header, r.Err = gophercloud.ParseResponse(resp, err)
	return
}

// UpdateRuleOptsBuilder allows to add additional parameters to the PUT request.
type UpdateRuleOptsBuilder interface {
	ToRuleUpdateMap() (map[string]any, error)
}

// UpdateRuleOpts is the common options struct used in this package's Update
// operation.
type UpdateRuleOpts struct {
	// The L7 rule type. One of COOKIE, FILE_TYPE, HEADER, HOST_NAME, or PATH.
	RuleType RuleType `json:"type,omitempty"`

	// The comparison type for the L7 rule. One of CONTAINS, ENDS_WITH, EQUAL_TO, REGEX, or STARTS_WITH.
	CompareType CompareType `json:"compare_type,omitempty"`

	// The value to use for the comparison. For example, the file type to compare.
	Value string `json:"value,omitempty"`

	// The key to use for the comparison. For example, the name of the cookie to evaluate.
	Key *string `json:"key,omitempty"`

	// When true the logic of the rule is inverted. For example, with invert true,
	// equal to would become not equal to. Default is false.
	Invert *bool `json:"invert,omitempty"`

	// The administrative state of the Loadbalancer. A valid value is true (UP)
	// or false (DOWN).
	AdminStateUp *bool `json:"admin_state_up,omitempty"`

	// Tags is a set of resource tags. Requires version 2.5.
	Tags *[]string `json:"tags,omitempty"`
}

// ToRuleUpdateMap builds a request body from UpdateRuleOpts.
func (opts UpdateRuleOpts) ToRuleUpdateMap() (map[string]any, error) {
	b, err := gophercloud.BuildRequestBody(opts, "rule")
	if err != nil {
		return nil, err
	}

	if m := b["rule"].(map[string]any); m["key"] == "" {
		m["key"] = nil
	}

	return b, nil
}

// UpdateRule allows Rule to be updated.
func UpdateRule(ctx context.Context, c *gophercloud.ServiceClient, policyID string, ruleID string, opts UpdateRuleOptsBuilder) (r UpdateRuleResult) {
	b, err := opts.ToRuleUpdateMap()
	if err != nil {
		r.Err = err
		return
	}
	resp, err := c.Put(ctx, ruleResourceURL(c, policyID, ruleID), b, &r.Body, &gophercloud.RequestOpts{
		OkCodes: []int{200, 201, 202},
	})
	_, r.Header, r.Err = gophercloud.ParseResponse(resp, err)
	return
}
