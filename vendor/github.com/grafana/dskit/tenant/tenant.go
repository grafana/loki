package tenant

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/grafana/dskit/user"
)

const (
	// MaxTenantIDLength is the max length of single tenant ID in bytes
	MaxTenantIDLength = 150

	tenantIDsSeparator = '|'

	// subtenantIDSeparator separates the tenant ID from the subtenant ID.
	// The format is "tenantID:subtenantID" (e.g., "123456:k6").
	// The colon is not a valid character in tenant IDs, making it safe to use as a separator.
	subtenantIDSeparator = ':'
)

var (
	// validTenantIdChars is a lookup table for valid tenant ID characters.
	validTenantIdChars [256]bool

	errTenantIDTooLong = fmt.Errorf("tenant ID is too long: max %d characters", MaxTenantIDLength)
	errUnsafeTenantID  = errors.New("tenant ID is '.' or '..'")
)

func init() {
	// letters
	for c := 'a'; c <= 'z'; c++ {
		validTenantIdChars[c] = true
	}
	for c := 'A'; c <= 'Z'; c++ {
		validTenantIdChars[c] = true
	}
	// digits
	for c := '0'; c <= '9'; c++ {
		validTenantIdChars[c] = true
	}
	// special characters: ! - _ . * ' ( )
	for _, c := range "!-_.*'()" {
		validTenantIdChars[c] = true
	}
}

type errTenantIDUnsupportedCharacter struct {
	pos      int
	tenantID string
}

func (e *errTenantIDUnsupportedCharacter) Error() string {
	return fmt.Sprintf(
		"tenant ID '%s' contains unsupported character '%c'",
		e.tenantID,
		e.tenantID[e.pos],
	)
}

// NormalizeTenantIDs creates a normalized form by sorting and de-duplicating the list of tenantIDs
func NormalizeTenantIDs(tenantIDs []string) []string {
	sort.Strings(tenantIDs)

	count := len(tenantIDs)
	if count <= 1 {
		return tenantIDs
	}

	posOut := 1
	for posIn := 1; posIn < count; posIn++ {
		if tenantIDs[posIn] != tenantIDs[posIn-1] {
			tenantIDs[posOut] = tenantIDs[posIn]
			posOut++
		}
	}

	return tenantIDs[0:posOut]
}

// ValidTenantID returns an error if the tenant ID is invalid, nil otherwise.
func ValidTenantID(s string) error {
	for i := 0; i < len(s); i++ {
		if !validTenantIdChars[s[i]] {
			return &errTenantIDUnsupportedCharacter{tenantID: s, pos: i}
		}
	}
	if len(s) > MaxTenantIDLength {
		return errTenantIDTooLong
	}
	if s == "." || s == ".." {
		return errUnsafeTenantID
	}
	return nil
}

// ValidSubtenantID returns an error if the subtenant ID is invalid, nil otherwise.
// Subtenant IDs follow the same rules as tenant IDs.
func ValidSubtenantID(s string) error {
	return ValidTenantID(s)
}

// JoinTenantIDs returns all tenant IDs concatenated with the separator character `|`
func JoinTenantIDs(tenantIDs []string) string {
	return strings.Join(tenantIDs, string(tenantIDsSeparator))
}

// ExtractTenantIDFromHTTPRequest extracts a single tenant ID directly from a HTTP request.
func ExtractTenantIDFromHTTPRequest(req *http.Request) (string, context.Context, error) {
	//lint:ignore faillint wrapper around upstream method
	_, ctx, err := user.ExtractOrgIDFromHTTPRequest(req)
	if err != nil {
		return "", nil, err
	}

	tenantID, err := TenantID(ctx)
	if err != nil {
		return "", nil, err
	}

	return tenantID, ctx, nil
}

// TenantIDsFromOrgID extracts different tenants from an orgID string value
//
// ignore stutter warning
//
//nolint:revive
func TenantIDsFromOrgID(orgID string) ([]string, error) {
	return TenantIDs(user.InjectOrgID(context.TODO(), orgID))
}

func trimSubtenantID(orgID string) string {
	idx := strings.IndexByte(orgID, subtenantIDSeparator)
	if idx == -1 {
		return orgID
	}
	return orgID[:idx]
}

// splitTenantAndSubtenant splits an orgID into tenant ID and subtenant ID.
// If the orgID contains no subtenant separator, the subtenant will be empty.
// The format is "tenantID:subtenantID" (e.g., "123456:k6").
func splitTenantAndSubtenant(orgID string) (tenantID, subtenantID string) {
	idx := strings.IndexByte(orgID, subtenantIDSeparator)
	if idx == -1 {
		return orgID, ""
	}
	return orgID[:idx], orgID[idx+1:]
}

// stringsCut is like strings.Cut but uses strings.IndexByte instead.
func stringsCut(s string, sep byte) (string, string, bool) {
	idx := strings.IndexByte(s, sep)
	if idx == -1 {
		return s, "", false
	}
	return s[:idx], s[idx+1:], true
}
