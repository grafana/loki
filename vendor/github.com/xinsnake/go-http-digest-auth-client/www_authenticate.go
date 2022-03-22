package digest_auth_client

import (
	"regexp"
	"strings"
)

type wwwAuthenticate struct {
	Algorithm string // unquoted
	Domain    string // quoted
	Nonce     string // quoted
	Opaque    string // quoted
	Qop       string // quoted
	Realm     string // quoted
	Stale     bool   // unquoted
	Charset   string // quoted
	Userhash  bool   // quoted
}

func newWwwAuthenticate(s string) *wwwAuthenticate {

	var wa = wwwAuthenticate{}

	algorithmRegex := regexp.MustCompile(`algorithm="([^ ,]+)"`)
	algorithmMatch := algorithmRegex.FindStringSubmatch(s)
	if algorithmMatch != nil {
		wa.Algorithm = algorithmMatch[1]
	}

	domainRegex := regexp.MustCompile(`domain="(.+?)"`)
	domainMatch := domainRegex.FindStringSubmatch(s)
	if domainMatch != nil {
		wa.Domain = domainMatch[1]
	}

	nonceRegex := regexp.MustCompile(`nonce="(.+?)"`)
	nonceMatch := nonceRegex.FindStringSubmatch(s)
	if nonceMatch != nil {
		wa.Nonce = nonceMatch[1]
	}

	opaqueRegex := regexp.MustCompile(`opaque="(.+?)"`)
	opaqueMatch := opaqueRegex.FindStringSubmatch(s)
	if opaqueMatch != nil {
		wa.Opaque = opaqueMatch[1]
	}

	qopRegex := regexp.MustCompile(`qop="(.+?)"`)
	qopMatch := qopRegex.FindStringSubmatch(s)
	if qopMatch != nil {
		wa.Qop = qopMatch[1]
	}

	realmRegex := regexp.MustCompile(`realm="(.+?)"`)
	realmMatch := realmRegex.FindStringSubmatch(s)
	if realmMatch != nil {
		wa.Realm = realmMatch[1]
	}

	staleRegex := regexp.MustCompile(`stale=([^ ,])"`)
	staleMatch := staleRegex.FindStringSubmatch(s)
	if staleMatch != nil {
		wa.Stale = (strings.ToLower(staleMatch[1]) == "true")
	}

	charsetRegex := regexp.MustCompile(`charset="(.+?)"`)
	charsetMatch := charsetRegex.FindStringSubmatch(s)
	if charsetMatch != nil {
		wa.Charset = charsetMatch[1]
	}

	userhashRegex := regexp.MustCompile(`userhash=([^ ,])"`)
	userhashMatch := userhashRegex.FindStringSubmatch(s)
	if userhashMatch != nil {
		wa.Userhash = (strings.ToLower(userhashMatch[1]) == "true")
	}

	return &wa
}
