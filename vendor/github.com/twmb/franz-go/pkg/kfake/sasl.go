package kfake

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/twmb/franz-go/pkg/kmsg"
	"golang.org/x/crypto/pbkdf2"
)

// TODO server-error-value in serverFinal

const (
	saslPlain       = "PLAIN"
	saslScram256    = "SCRAM-SHA-256"
	saslScram512    = "SCRAM-SHA-512"
	scramIterations = 4096
)

type (
	sasls struct {
		plain    map[string]string    // user => pass
		scram256 map[string]scramAuth // user => scram auth
		scram512 map[string]scramAuth // user => scram auth
	}

	saslStage uint8
)

func (s sasls) empty() bool {
	return len(s.plain) == 0 && len(s.scram256) == 0 && len(s.scram512) == 0
}

const (
	saslStageBegin saslStage = iota
	saslStageAuthPlain
	saslStageAuthScram0_256
	saslStageAuthScram0_512
	saslStageAuthScram1
	saslStageComplete
)

func (c *Cluster) handleSASL(creq *clientReq) (allow bool) {
	switch creq.cc.saslStage {
	case saslStageBegin:
		switch creq.kreq.(type) {
		case *kmsg.ApiVersionsRequest,
			*kmsg.SASLHandshakeRequest:
			return true
		default:
			return false
		}
	case saslStageAuthPlain,
		saslStageAuthScram0_256,
		saslStageAuthScram0_512,
		saslStageAuthScram1:
		switch creq.kreq.(type) {
		case *kmsg.ApiVersionsRequest,
			*kmsg.SASLAuthenticateRequest:
			return true
		default:
			return false
		}
	case saslStageComplete:
		return true
	default:
		panic("unreachable")
	}
}

///////////
// PLAIN //
///////////

func saslSplitPlain(auth []byte) (user, pass string, err error) {
	parts := strings.SplitN(string(auth), "\x00", 3)
	if len(parts) != 3 {
		return "", "", errors.New("invalid plain auth")
	}
	if len(parts[0]) != 0 && parts[0] != parts[1] {
		return "", "", errors.New("authzid is not equal to username") // see below
	}
	return parts[1], parts[2], nil
}

///////////
// SCRAM //
///////////

func newScramAuth(mechanism, pass string) scramAuth {
	var saltedPass []byte
	salt := randBytes(10)
	switch mechanism {
	case saslScram256:
		saltedPass = pbkdf2.Key([]byte(pass), salt, scramIterations, sha256.Size, sha256.New)
	case saslScram512:
		saltedPass = pbkdf2.Key([]byte(pass), salt, scramIterations, sha512.Size, sha512.New)
	default:
		panic("unreachable")
	}
	return scramAuth{
		mechanism:  mechanism,
		iterations: scramIterations,
		saltedPass: saltedPass,
		salt:       salt,
	}
}

type scramAuth struct {
	mechanism  string // scram 256 or 512
	iterations int
	saltedPass []byte
	salt       []byte
}

// client-first-message
type scramClient0 struct {
	user  string
	bare  []byte // client-first-message-bare
	nonce []byte // nonce in client0
}

var scramUnescaper = strings.NewReplacer("=3D", "=", "=2C", ",")

func scramParseClient0(client0 []byte) (scramClient0, error) {
	m := reClient0.FindSubmatch(client0)
	if len(m) == 0 {
		return scramClient0{}, errors.New("invalid client0")
	}
	var (
		zid   = string(m[1])
		bare  = bytes.Clone(m[2])
		user  = string(m[3])
		nonce = bytes.Clone(m[4])
		ext   = string(m[5])
	)
	if len(ext) != 0 {
		return scramClient0{}, errors.New("invalid extensions")
	}
	if zid != "" && zid != user {
		return scramClient0{}, errors.New("authzid is not equal to username") // Kafka & Redpanda enforce that a present zid == username
	}
	return scramClient0{
		user:  scramUnescaper.Replace(user),
		bare:  bare,
		nonce: nonce,
	}, nil
}

func scramServerFirst(client0 scramClient0, auth scramAuth) (scramServer0, []byte) {
	nonce := append(client0.nonce, base64.RawStdEncoding.EncodeToString(randBytes(16))...)
	serverFirst := []byte(fmt.Sprintf("r=%s,s=%s,i=%d",
		nonce,
		base64.StdEncoding.EncodeToString(auth.salt),
		scramIterations,
	))
	return scramServer0{
		a:      auth,
		c0bare: client0.bare,
		s0:     serverFirst,
	}, serverFirst
}

// server-first-message
type scramServer0 struct {
	a      scramAuth
	c0bare []byte
	s0     []byte
}

// validates client-final-message and replies with server-final-message
func (s *scramServer0) serverFinal(clientFinal []byte) ([]byte, error) {
	m := reClientFinal.FindSubmatch(clientFinal)
	if len(m) == 0 {
		return nil, errors.New("invalid client-final-message")
	}
	var (
		finalWithoutProof = m[1]
		channel           = m[2]
		clientProof64     = m[3]
		h                 = sha256.New
	)
	if s.a.mechanism == saslScram512 {
		h = sha512.New
	}
	if !bytes.Equal(channel, []byte("biws")) { // "biws" == base64("n,,")
		return nil, errors.New("invalid channel binding")
	}
	clientProof, err := base64.StdEncoding.DecodeString(string(clientProof64))
	if err != nil {
		return nil, errors.New("client proof is not std-base64")
	}
	if len(clientProof) != h().Size() {
		return nil, fmt.Errorf("len(client proof) %d != expected %d", len(clientProof), h().Size())
	}

	var clientKey []byte // := HMAC(SaltedPass, "Client Key")
	{
		mac := hmac.New(h, s.a.saltedPass)
		mac.Write([]byte("Client Key"))
		clientKey = mac.Sum(nil)
	}

	var storedKey []byte // := H(ClientKey)
	{
		h := h()
		h.Write(clientKey)
		storedKey = h.Sum(nil)
	}

	var authMessage []byte // := client-first-bare-message + "," + server-first-message + "," + client-final-message-without-proof
	{
		authMessage = append(s.c0bare, ',')
		authMessage = append(authMessage, s.s0...)
		authMessage = append(authMessage, ',')
		authMessage = append(authMessage, finalWithoutProof...)
	}

	var clientSignature []byte // := HMAC(StoredKey, AuthMessage)
	{
		mac := hmac.New(h, storedKey)
		mac.Write(authMessage)
		clientSignature = mac.Sum(nil)
	}

	usedKey := clientProof // := ClientKey XOR ClientSignature
	{
		for i, b := range clientSignature {
			usedKey[i] ^= b
		}
		h := h()
		h.Write(usedKey)
		usedKey = h.Sum(nil)
	}
	if !bytes.Equal(usedKey, storedKey) {
		return nil, errors.New("invalid password")
	}

	var serverKey []byte // := HMAC(SaltedPass, "Server Key")
	{
		mac := hmac.New(h, s.a.saltedPass)
		mac.Write([]byte("Server Key"))
		serverKey = mac.Sum(nil)
	}
	var serverSignature []byte // := HMAC(ServerKey, AuthMessage)
	{
		mac := hmac.New(h, serverKey)
		mac.Write(authMessage)
		serverSignature = mac.Sum(nil)
	}

	serverFinal := []byte(fmt.Sprintf("v=%s", base64.StdEncoding.EncodeToString(serverSignature)))
	return serverFinal, nil
}

var reClient0, reClientFinal *regexp.Regexp

func init() {
	// https://datatracker.ietf.org/doc/html/rfc5802#section-7
	const (
		valueSafe = "[\x01-\x2b\x2d-\x3c\x3e-\x7f]+"             // all except \0 - ,
		value     = "[\x01-\x2b\x2d-\x7f]+"                      // all except \0 ,
		printable = "[\x21-\x2b\x2d-\x7e]+"                      // all except , (and DEL, unnoted)
		saslName  = "(?:[\x01-\x2b\x2d-\x3c\x3e-\x7f]|=2C|=3D)+" // valueSafe | others; kafka is lazy here
		b64       = `[a-zA-Z0-9/+]+={0,3}`                       // we are lazy here matching up to 3 =
		ext       = "(?:,[a-zA-Z]+=[\x01-\x2b\x2d-\x7f]+)*"
	)

	// 0: entire match
	// 1: authzid
	// 2: client-first-message-bare
	// 3: username
	// 4: nonce
	// 5: ext
	client0 := fmt.Sprintf("^n,(?:a=(%s))?,((?:m=%s,)?n=(%s),r=(%s)(%s))$", saslName, value, saslName, printable, ext)

	// We reject extensions in client0. Kafka does not validate the nonce
	// and some clients may generate it incorrectly (i.e. old franz-go), so
	// we do not validate it.
	//
	// 0: entire match
	// 1: channel-final-message-without-proof
	// 2: channel binding
	// 3: proof
	clientFinal := fmt.Sprintf("^(c=(%s),r=%s),p=(%s)$", b64, printable, b64)

	reClient0 = regexp.MustCompile(client0)
	reClientFinal = regexp.MustCompile(clientFinal)
}
