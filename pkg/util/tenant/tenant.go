package tenant

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/weaveworks/common/user"
)

var (
	errTenantIDTooLong = errors.New("tenant ID is too long: max 150 characters")
)

var defaultResolver Resolver = &singleResolver{}

// ID returns exactly a single tenant ID from the context. It should be
// used when a certain endpoint should only support exactly a single
// tenant ID. It returns an error user.ErrNoOrgID if there is no tenant ID
// supplied or user.ErrTooManyOrgIDs if there are multiple tenant IDs present.
func ID(ctx context.Context) (string, error) {
	return defaultResolver.TenantID(ctx)
}

// IDs returns all tenant IDs from the context. It should return a
// normalized list of ordered and distinct tenant IDs (as produced by
// normalizeTenantIDs).
func IDs(ctx context.Context) ([]string, error) {
	return defaultResolver.TenantIDs(ctx)
}

type Resolver interface {
	// TenantID returns exactly a single tenant ID from the context. It should be
	// used when a certain endpoint should only support exactly a single
	// tenant ID. It returns an error user.ErrNoOrgID if there is no tenant ID
	// supplied or user.ErrTooManyOrgIDs if there are multiple tenant IDs present.
	TenantID(context.Context) (string, error)

	// TenantIDs returns all tenant IDs from the context. It should return
	// normalized list of ordered and distinct tenant IDs (as produced by
	// normalizeTenantIDs).
	TenantIDs(context.Context) ([]string, error)
}

type singleResolver struct {
}

// containsUnsafePathSegments will return true if the string is a directory
// reference like `.` and `..` or if any path separator character like `/` and
// `\` can be found.
func containsUnsafePathSegments(id string) bool {
	// handle the relative reference to current and parent path.
	if id == "." || id == ".." {
		return true
	}

	return strings.ContainsAny(id, "\\/")
}

var errInvalidTenantID = errors.New("invalid tenant ID")

func (t *singleResolver) TenantID(ctx context.Context) (string, error) {
	//lint:ignore faillint wrapper around upstream method
	id, err := user.ExtractOrgID(ctx)
	if err != nil {
		return "", err
	}

	if containsUnsafePathSegments(id) {
		return "", errInvalidTenantID
	}

	return id, nil
}

func (t *singleResolver) TenantIDs(ctx context.Context) ([]string, error) {
	orgID, err := t.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	return []string{orgID}, err
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

func validTenantID(s string) error {
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
