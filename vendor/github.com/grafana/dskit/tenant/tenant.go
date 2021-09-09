package tenant

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/weaveworks/common/user"
)

var (
	errTenantIDTooLong = errors.New("tenant ID is too long: max 150 characters")
)

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

const tenantIDsLabelSeparator = "|"

// NormalizeTenantIDs is creating a normalized form by sortiing and de-duplicating the list of tenantIDs
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

// ValidTenantID
func ValidTenantID(s string) error {
	// check if it contains invalid runes
	for pos, r := range s {
		if !isSupported(r) {
			return &errTenantIDUnsupportedCharacter{
				tenantID: s,
				pos:      pos,
			}
		}
	}

	if len(s) > 150 {
		return errTenantIDTooLong
	}

	return nil
}

func JoinTenantIDs(tenantIDs []string) string {
	return strings.Join(tenantIDs, tenantIDsLabelSeparator)
}

// this checks if a rune is supported in tenant IDs (according to
// https://cortexmetrics.io/docs/guides/limitations/#tenant-id-naming)
func isSupported(c rune) bool {
	// characters
	if ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') {
		return true
	}

	// digits
	if '0' <= c && c <= '9' {
		return true
	}

	// special
	return c == '!' ||
		c == '-' ||
		c == '_' ||
		c == '.' ||
		c == '*' ||
		c == '\'' ||
		c == '(' ||
		c == ')'
}

// IDsFromOrgID extracts different tenants from an orgID string value
func IDsFromOrgID(orgID string) ([]string, error) {
	return IDs(user.InjectOrgID(context.TODO(), orgID))
}
