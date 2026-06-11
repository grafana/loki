// Package scram provides SCRAM-SHA-256 and SCRAM-SHA-512 sasl authentication
// as specified in RFC5802.
package scram

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"fmt"
	"hash"
	"strconv"
	"strings"

	"golang.org/x/crypto/pbkdf2"

	"github.com/twmb/franz-go/pkg/sasl"
)

// Auth contains information for authentication.
//
// This client may add fields to this struct in the future if Kafka adds more
// extensions to SCRAM.
type Auth struct {
	// Zid is an optional authorization ID to use in authenticating.
	Zid string

	// User is username to use for authentication.
	//
	// Note that this package does not attempt to "prepare" the username
	// for authentication; this package assumes that the incoming username
	// has already been prepared / does not need preparing.
	//
	// Preparing simply normalizes case / removes invalid characters; doing
	// so is likely not necessary.
	User string

	// Pass is the password to use for authentication.
	Pass string

	// Nonce, if provided, is the nonce to use for authentication. If not
	// provided, this package uses 20 bytes read with crypto/rand.
	Nonce []byte

	// IsToken, if true, suffixes the "tokenauth=true" extra attribute to
	// the initial authentication message.
	//
	// Set this to true if the user and pass are from a delegation token.
	IsToken bool

	_ struct{} // require explicit field initialization
}

// AsSha256Mechanism returns a sasl mechanism that will use 'a' as credentials
// for all sasl sessions.
//
// This is a shortcut for using the Sha256 function and is useful when you do
// not need to live-rotate credentials.
func (a Auth) AsSha256Mechanism() sasl.Mechanism {
	return Sha256(func(context.Context) (Auth, error) {
		return a, nil
	})
}

// AsSha512Mechanism returns a sasl mechanism that will use 'a' as credentials
// for all sasl sessions.
//
// This is a shortcut for using the Sha512 function and is useful when you do
// not need to live-rotate credentials.
func (a Auth) AsSha512Mechanism() sasl.Mechanism {
	return Sha512(func(context.Context) (Auth, error) {
		return a, nil
	})
}

// Sha256 returns a SCRAM-SHA-256 sasl mechanism that will call authFn
// whenever authentication is needed. The returned Auth is used for a single
// session.
func Sha256(authFn func(context.Context) (Auth, error)) sasl.Mechanism {
	return scram{authFn, sha256.New, "SCRAM-SHA-256"}
}

// Sha512 returns a SCRAM-SHA-512 sasl mechanism that will call authFn
// whenever authentication is needed. The returned Auth is used for a single
// session.
func Sha512(authFn func(context.Context) (Auth, error)) sasl.Mechanism {
	return scram{authFn, sha512.New, "SCRAM-SHA-512"}
}

type scram struct {
	authFn  func(context.Context) (Auth, error)
	newhash func() hash.Hash
	name    string
}

var escaper = strings.NewReplacer("=", "=3D", ",", "=2C")

func (s scram) Name() string { return s.name }
func (s scram) Authenticate(ctx context.Context, _ string) (sasl.Session, []byte, error) {
	auth, err := s.authFn(ctx)
	if err != nil {
		return nil, nil, err
	}
	if auth.User == "" || auth.Pass == "" {
		return nil, nil, errors.New(s.name + " user and pass must be non-empty")
	}
	if len(auth.Nonce) == 0 {
		buf := make([]byte, 20)
		if _, err = rand.Read(buf); err != nil {
			return nil, nil, err
		}
		auth.Nonce = buf
	}

	auth.Nonce = []byte(base64.RawStdEncoding.EncodeToString(auth.Nonce))

	clientFirstMsgBare := make([]byte, 0, 100)
	clientFirstMsgBare = append(clientFirstMsgBare, "n="...)
	clientFirstMsgBare = append(clientFirstMsgBare, escaper.Replace(auth.User)...)
	clientFirstMsgBare = append(clientFirstMsgBare, ",r="...)
	clientFirstMsgBare = append(clientFirstMsgBare, auth.Nonce...)
	if auth.IsToken {
		clientFirstMsgBare = append(clientFirstMsgBare, ",tokenauth=true"...) // KIP-48
	}

	gs2Header := "n," // no channel binding
	if auth.Zid != "" {
		gs2Header += "a=" + escaper.Replace(auth.Zid)
	}
	gs2Header += ","
	clientFirstMsg := append([]byte(gs2Header), clientFirstMsgBare...)
	return &session{
		step:    0,
		auth:    auth,
		newhash: s.newhash,

		clientFirstMsgBare: clientFirstMsgBare,
	}, clientFirstMsg, nil
}

type session struct {
	step    int
	auth    Auth
	newhash func() hash.Hash

	clientFirstMsgBare []byte
	expServerSignature []byte
}

func (s *session) Challenge(resp []byte) (bool, []byte, error) {
	step := s.step
	s.step++
	switch step {
	case 0:
		response, err := s.authenticateClient(resp)
		return false, response, err
	case 1:
		err := s.verifyServer(resp)
		return err == nil, nil, err
	default:
		return false, nil, fmt.Errorf("challenge / response should be done, but still going at %d", step)
	}
}

// server-first-message = [reserved-mext ","] nonce "," salt "," iteration-count ["," extensions]
// we ignore extensions
func (s *session) authenticateClient(serverFirstMsg []byte) ([]byte, error) {
	kvs := bytes.Split(serverFirstMsg, []byte(","))
	if len(kvs) < 3 {
		return nil, fmt.Errorf("got %d kvs != exp min 3", len(kvs))
	}

	// NONCE
	if !bytes.HasPrefix(kvs[0], []byte("r=")) {
		return nil, fmt.Errorf("unexpected kv %q where nonce expected", kvs[0])
	}
	serverNonce := kvs[0][2:]
	if !bytes.HasPrefix(serverNonce, s.auth.Nonce) {
		return nil, errors.New("server did not reply with nonce beginning with client nonce")
	}

	// SALT
	if !bytes.HasPrefix(kvs[1], []byte("s=")) {
		return nil, fmt.Errorf("unexpected kv %q where salt expected", kvs[1])
	}
	salt, err := base64.StdEncoding.DecodeString(string(kvs[1][2:]))
	if err != nil {
		return nil, fmt.Errorf("server salt %q decode err: %v", kvs[1][2:], err)
	}

	// ITERATIONS
	if !bytes.HasPrefix(kvs[2], []byte("i=")) {
		return nil, fmt.Errorf("unexpected kv %q where iterations expected", kvs[2])
	}
	iters, err := strconv.Atoi(string(kvs[2][2:]))
	if err != nil {
		return nil, fmt.Errorf("server iterations %q parse err: %v", kvs[2][2:], err)
	}
	if iters < 4096 {
		return nil, fmt.Errorf("server iterations %d less than minimum 4096", iters)
	}

	//////////////////
	// CALCULATIONS //
	//////////////////

	h := s.newhash()
	saltedPassword := pbkdf2.Key([]byte(s.auth.Pass), salt, iters, h.Size(), s.newhash) // SaltedPassword := Hi(Normalize(password), salt, i)

	mac := hmac.New(s.newhash, saltedPassword)
	if _, err = mac.Write([]byte("Client Key")); err != nil {
		return nil, fmt.Errorf("hmac err: %v", err)
	}
	clientKey := mac.Sum(nil) // ClientKey := HMAC(SaltedPassword, "Client Key")
	if _, err = h.Write(clientKey); err != nil {
		return nil, fmt.Errorf("sha err: %v", err)
	}
	storedKey := h.Sum(nil) // StoredKey := H(ClientKey)

	// biws is `n,,` base64 encoded; we do not use a channel
	clientFinalMsgWithoutProof := append([]byte("c=biws,r="), serverNonce...)
	authMsg := append(s.clientFirstMsgBare, ',')             // AuthMsg := client-first-message-bare + "," +
	authMsg = append(authMsg, serverFirstMsg...)             //            server-first-message +
	authMsg = append(authMsg, ',')                           //            "," +
	authMsg = append(authMsg, clientFinalMsgWithoutProof...) //            client-final-message-without-proof

	mac = hmac.New(s.newhash, storedKey)
	if _, err = mac.Write(authMsg); err != nil {
		return nil, fmt.Errorf("hmac err: %v", err)
	}
	clientSignature := mac.Sum(nil) // ClientSignature := HMAC(StoredKey, AuthMessage)

	clientProof := clientSignature
	for i, c := range clientKey {
		clientProof[i] ^= c // ClientProof := ClientKey XOR ClientSignature
	}

	mac = hmac.New(s.newhash, saltedPassword)
	if _, err = mac.Write([]byte("Server Key")); err != nil {
		return nil, fmt.Errorf("hmac err: %v", err)
	}
	serverKey := mac.Sum(nil) // ServerKey := HMAC(SaltedPassword, "Server Key")
	mac = hmac.New(s.newhash, serverKey)
	if _, err = mac.Write(authMsg); err != nil {
		return nil, fmt.Errorf("hmac err: %v", err)
	}
	s.expServerSignature = []byte(base64.StdEncoding.EncodeToString(mac.Sum(nil))) // ServerSignature := HMAC(ServerKey, AuthMessage)

	clientFinalMsg := append(clientFinalMsgWithoutProof, ",p="...)
	clientFinalMsg = append(clientFinalMsg, base64.StdEncoding.EncodeToString(clientProof)...)
	return clientFinalMsg, nil
}

func (s *session) verifyServer(serverFinalMsg []byte) error {
	kvs := bytes.Split(serverFinalMsg, []byte(","))
	if len(kvs) < 1 {
		return errors.New("received no kvs, even though this should be impossible")
	}

	kv := kvs[0]
	if isErr := bytes.HasPrefix(kv, []byte("e=")); isErr {
		return fmt.Errorf("server sent authentication error %q", kv[2:])
	}
	if !bytes.HasPrefix(kv, []byte("v=")) {
		return fmt.Errorf("server sent unexpected first kv %q", kv)
	}
	if !bytes.Equal(s.expServerSignature, kv[2:]) {
		return fmt.Errorf("server signature mismatch; got %q != exp %q", kv[2:], s.expServerSignature)
	}
	return nil
}
