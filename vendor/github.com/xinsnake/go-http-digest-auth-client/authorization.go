package digest_auth_client

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/url"
	"strings"
	"time"
)

type authorization struct {
	Algorithm string // unquoted
	Cnonce    string // quoted
	Nc        int    // unquoted
	Nonce     string // quoted
	Opaque    string // quoted
	Qop       string // unquoted
	Realm     string // quoted
	Response  string // quoted
	URI       string // quoted
	Userhash  bool   // quoted
	Username  string // quoted
	Username_ string // quoted
}

func newAuthorization(dr *DigestRequest) (*authorization, error) {

	ah := authorization{
		Algorithm: dr.Wa.Algorithm,
		Cnonce:    "",
		Nc:        0,
		Nonce:     dr.Wa.Nonce,
		Opaque:    dr.Wa.Opaque,
		Qop:       "",
		Realm:     dr.Wa.Realm,
		Response:  "",
		URI:       "",
		Userhash:  dr.Wa.Userhash,
		Username:  "",
		Username_: "", // TODO
	}

	return ah.refreshAuthorization(dr)
}

const (
	algorithmMD5        = "MD5"
	algorithmMD5Sess    = "MD5-SESS"
	algorithmSHA256     = "SHA-256"
	algorithmSHA256Sess = "SHA-256-SESS"
)

func (ah *authorization) refreshAuthorization(dr *DigestRequest) (*authorization, error) {

	ah.Username = dr.Username

	if ah.Userhash {
		ah.Username = ah.hash(fmt.Sprintf("%s:%s", ah.Username, ah.Realm))
	}

	ah.Nc++

	ah.Cnonce = ah.hash(fmt.Sprintf("%d:%s:my_value", time.Now().UnixNano(), dr.Username))

	url, err := url.Parse(dr.URI)
	if err != nil {
		return nil, err
	}

	ah.URI = url.RequestURI()
	ah.Response = ah.computeResponse(dr)

	return ah, nil
}

func (ah *authorization) computeResponse(dr *DigestRequest) (s string) {

	kdSecret := ah.hash(ah.computeA1(dr))
	kdData := fmt.Sprintf("%s:%08x:%s:%s:%s", ah.Nonce, ah.Nc, ah.Cnonce, ah.Qop, ah.hash(ah.computeA2(dr)))

	return ah.hash(fmt.Sprintf("%s:%s", kdSecret, kdData))
}

func (ah *authorization) computeA1(dr *DigestRequest) string {

	algorithm := strings.ToUpper(ah.Algorithm)

	if algorithm == "" || algorithm == algorithmMD5 || algorithm == algorithmSHA256 {
		return fmt.Sprintf("%s:%s:%s", ah.Username, ah.Realm, dr.Password)
	}

	if algorithm == algorithmMD5Sess || algorithm == algorithmSHA256Sess {
		upHash := ah.hash(fmt.Sprintf("%s:%s:%s", ah.Username, ah.Realm, dr.Password))
		return fmt.Sprintf("%s:%s:%s", upHash, ah.Nonce, ah.Cnonce)
	}

	return ""
}

func (ah *authorization) computeA2(dr *DigestRequest) string {

	if strings.Contains(dr.Wa.Qop, "auth-int") {
		ah.Qop = "auth-int"
		return fmt.Sprintf("%s:%s:%s", dr.Method, ah.URI, ah.hash(dr.Body))
	}

	if dr.Wa.Qop == "auth" || dr.Wa.Qop == "" {
		ah.Qop = "auth"
		return fmt.Sprintf("%s:%s", dr.Method, ah.URI)
	}

	return ""
}

func (ah *authorization) hash(a string) string {
	var h hash.Hash
	algorithm := strings.ToUpper(ah.Algorithm)

	if algorithm == "" || algorithm == algorithmMD5 || algorithm == algorithmMD5Sess {
		h = md5.New()
	} else if algorithm == algorithmSHA256 || algorithm == algorithmSHA256Sess {
		h = sha256.New()
	} else {
		// unknown algorithm
		return ""
	}

	io.WriteString(h, a)
	return hex.EncodeToString(h.Sum(nil))
}

func (ah *authorization) toString() string {
	var buffer bytes.Buffer

	buffer.WriteString("Digest ")

	if ah.Username != "" {
		buffer.WriteString(fmt.Sprintf("username=\"%s\", ", ah.Username))
	}

	if ah.Realm != "" {
		buffer.WriteString(fmt.Sprintf("realm=\"%s\", ", ah.Realm))
	}

	if ah.Nonce != "" {
		buffer.WriteString(fmt.Sprintf("nonce=\"%s\", ", ah.Nonce))
	}

	if ah.URI != "" {
		buffer.WriteString(fmt.Sprintf("uri=\"%s\", ", ah.URI))
	}

	if ah.Response != "" {
		buffer.WriteString(fmt.Sprintf("response=\"%s\", ", ah.Response))
	}

	if ah.Algorithm != "" {
		buffer.WriteString(fmt.Sprintf("algorithm=%s, ", ah.Algorithm))
	}

	if ah.Cnonce != "" {
		buffer.WriteString(fmt.Sprintf("cnonce=\"%s\", ", ah.Cnonce))
	}

	if ah.Opaque != "" {
		buffer.WriteString(fmt.Sprintf("opaque=\"%s\", ", ah.Opaque))
	}

	if ah.Qop != "" {
		buffer.WriteString(fmt.Sprintf("qop=%s, ", ah.Qop))
	}

	if ah.Nc != 0 {
		buffer.WriteString(fmt.Sprintf("nc=%08x, ", ah.Nc))
	}

	if ah.Userhash {
		buffer.WriteString("userhash=true, ")
	}

	s := buffer.String()

	return strings.TrimSuffix(s, ", ")
}
