package labelaccess

import (
	"context"
	"errors"
	"fmt"
	"hash/adler32"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/go-kit/log/level"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/v3/pkg/labelaccess/types"

	"github.com/grafana/loki/v3/pkg/util/httpreq"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const (
	// HTTPHeaderKey is used as the header to store label matcher values
	HTTPHeaderKey = "X-Prom-Label-Policy"
)

var (
	errNoMatcherSource     = errors.New("no label matcher source")
	errInvalidHeaderParse  = errors.New("invalid label policy header value, unable to parse tenant and selector")
	errInvalidHeaderEscape = errors.New("invalid label policy header value unable to url escape selector string")
	errInvalidSelector     = errors.New("invalid label policy header value unable to parse selector string")
)

// LabelPolicySet is used
type LabelPolicySet map[string][]*types.LabelPolicy

// hash iterates through the tenant map in ascending order and returns a base32 string of
// and adler hash of all of the tenants
func (l LabelPolicySet) hash() string {
	tenants := make([]string, 0, len(l))
	for t := range l {
		tenants = append(tenants, t)
	}
	h := adler32.New()
	sort.Strings(tenants)
	for _, t := range tenants {
		_, _ = h.Write([]byte(t))
		for _, p := range l[t] {
			data := p.String()
			data = data[:len(data)-1]
			_, _ = h.Write([]byte(p.String()))
		}
	}
	return fmt.Sprintf("%d", h.Sum32())
}

func (l LabelPolicySet) String() string {
	sb := strings.Builder{}

	tenants := make([]string, 0, len(l))
	for t := range l {
		tenants = append(tenants, t)
	}
	sort.Strings(tenants)

	for _, t := range tenants {
		sb.Write([]byte(t))
		sb.WriteString(":[")
		for _, p := range l[t] {
			sb.WriteString("{")
			for _, lm := range p.Selector {
				plm, err := types.LabelMatcherToPromLabel(lm)
				if err != nil {
					_ = level.Error(util_log.Logger).Log("msg", "unexpected error transforming LabelMatcher", "err", err)
				}
				sb.WriteString(plm.String())
				sb.WriteString(",")
			}
			sb.WriteString("},")
		}
		sb.WriteString("] ")
	}
	return sb.String()
}

func policyToHeaderValue(instanceName string, policy *types.LabelPolicy) (string, error) {
	matchers := make([]string, len(policy.Selector))

	for i := range policy.Selector {
		matcher, err := types.LabelMatcherToPromLabel(policy.Selector[i])
		if err != nil {
			return "", err
		}
		matchers[i] = matcher.String()
	}

	return instanceName + ":" + url.PathEscape("{"+strings.Join(matchers, ", ")+"}"), nil
}

// PolicyFromSelectorString is a helper function used for test fixtures. It should not be
// fed user supplied input as it panics on error
func PolicyFromSelectorString(policyString string) types.LabelPolicy {
	headerString := "dummy:" + url.PathEscape(policyString)
	_, policy, err := policyFromHeaderValue(headerString)
	if err != nil {
		panic(err)
	}
	return *policy
}

func policyFromHeaderValue(headerString string) (string, *types.LabelPolicy, error) {
	components := strings.SplitN(headerString, ":", 2)
	if len(components) != 2 {
		return "", nil, errInvalidHeaderParse
	}
	selectorString, err := url.PathUnescape(components[1])
	if err != nil {
		return "", nil, fmt.Errorf("%w, '%v'", errInvalidHeaderEscape, err)
	}
	matchers, err := parser.NewParser(parser.Options{}).ParseMetricSelector(selectorString)
	if err != nil {
		return "", nil, fmt.Errorf("%w '%s', error: '%v'", errInvalidSelector, selectorString, err)
	}

	policy := &types.LabelPolicy{
		Selector: make([]*types.LabelMatcher, len(matchers)),
	}
	for i, matcher := range matchers {
		policy.Selector[i], err = types.LabelMatcherFromPromLabel(matcher)
		if err != nil {
			return "", nil, err
		}
	}

	return components[0], policy, nil
}

// InjectLabelMatchersHTTP takes the provided matcher protobufs and stores them as HTTP headers
func InjectLabelMatchersHTTP(r *http.Request, instancePolicyMap LabelPolicySet) error {
	_ = level.Debug(util_log.Logger).Log("msg", "injecting label policy into HTTP request")

	// Ensure any existing policy sets are erased
	r.Header.Del(HTTPHeaderKey)
	for instanceName, policies := range instancePolicyMap {
		for _, policy := range policies {
			encodedPolicy, err := policyToHeaderValue(instanceName, policy)
			if err != nil {
				return err
			}
			r.Header.Add(HTTPHeaderKey, encodedPolicy)
		}
	}
	return nil
}

// ExtractLabelMatchersHTTP takes the provided HTTP request and parses out label matcher protobufs
func ExtractLabelMatchersHTTP(r *http.Request) (LabelPolicySet, error) {
	return ExtractLabelMatchersFromHeaders(r.Header)
}

// ExtractLabelMatchersFromHeaders parses label matcher protobufs from the provided headers
func ExtractLabelMatchersFromHeaders(headers http.Header) (LabelPolicySet, error) {
	headerValues := headers.Values(HTTPHeaderKey)

	instancePolicyMap := LabelPolicySet{}

	// Iterate through each set header value
	for _, headerValue := range headerValues {
		// Split the header value on a comma and iterate through each policy.
		for _, v := range strings.Split(headerValue, ",") {
			instanceName, policy, err := policyFromHeaderValue(v)
			if err != nil {
				return nil, err
			}

			currentPolicies, exists := instancePolicyMap[instanceName]
			if exists {
				instancePolicyMap[instanceName] = append(currentPolicies, policy)
			} else {
				instancePolicyMap[instanceName] = []*types.LabelPolicy{policy}
			}
		}
	}

	return instancePolicyMap, nil
}

// ExtractLabelMatchersContext gets the embedded label matchers from the context.
// It extracts headers stored by PropagateAllHeadersMiddleware or httpreq.InjectHeader.
func ExtractLabelMatchersContext(ctx context.Context) (LabelPolicySet, error) {
	headers := httpreq.ExtractAllHeaders(ctx)
	if headers == nil {
		return nil, errNoMatcherSource
	}
	return ExtractLabelMatchersFromHeaders(headers)
}

// InjectLabelMatchersContext injects label matchers into the context using httpreq.InjectHeader.
// This ensures the headers are propagated through the V2 engine's scheduler/worker mechanism.
func InjectLabelMatchersContext(ctx context.Context, matchers LabelPolicySet) context.Context {
	// Collect all encoded policies first, then join with comma.
	// httpreq.InjectHeader uses Set() which replaces, so we must inject all values at once.
	var encodedPolicies []string
	for instanceName, policies := range matchers {
		for _, policy := range policies {
			encoded, err := policyToHeaderValue(instanceName, policy)
			if err != nil {
				continue
			}
			encodedPolicies = append(encodedPolicies, encoded)
		}
	}

	if len(encodedPolicies) > 0 {
		combinedValue := strings.Join(encodedPolicies, ",")
		ctx = httpreq.InjectHeader(ctx, HTTPHeaderKey, combinedValue)
	}

	return ctx
}
