package spiffeid

import (
	"fmt"
	"strings"
)

// Matcher is used to match a SPIFFE ID.
type Matcher func(ID) error

// MatchAny matches any SPIFFE ID.
func MatchAny() Matcher {
	return Matcher(func(actual ID) error {
		return nil
	})
}

// MatchID matches a specific SPIFFE ID.
func MatchID(expected ID) Matcher {
	return Matcher(func(actual ID) error {
		if actual != expected {
			return fmt.Errorf("unexpected ID %q", actual)
		}
		return nil
	})
}

// MatchIDPrefix matches any SPIFFE ID with the given ID prefix. A matching ID
// must be in the same trust domain and have either the same path as the prefix
// or a path with the prefix on a segment boundary. If the prefix has no path,
// no ID matches.
func MatchIDPrefix(expected ID) Matcher {
	if expected.Path() == "" {
		return Matcher(func(actual ID) error {
			return errPrefixMissingPath
		})
	}
	return Matcher(func(actual ID) error {
		if actual.MemberOf(expected.TrustDomain()) && matchPathPrefix(actual.Path(), expected.Path()) {
			return nil
		}
		return fmt.Errorf("unexpected ID %q", actual)
	})
}

// MatchOneOf matches any SPIFFE ID in the given list of IDs.
func MatchOneOf(expected ...ID) Matcher {
	set := make(map[ID]struct{})
	for _, id := range expected {
		set[id] = struct{}{}
	}
	return Matcher(func(actual ID) error {
		if _, ok := set[actual]; !ok {
			return fmt.Errorf("unexpected ID %q", actual)
		}
		return nil
	})
}

// MatchMemberOf matches any SPIFFE ID in the given trust domain.
func MatchMemberOf(expected TrustDomain) Matcher {
	return Matcher(func(actual ID) error {
		if !actual.MemberOf(expected) {
			return fmt.Errorf("unexpected trust domain %q", actual.TrustDomain())
		}
		return nil
	})
}

func matchPathPrefix(actual, expected string) bool {
	return actual == expected || strings.HasPrefix(actual, expected+"/")
}
