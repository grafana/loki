package utils

import (
	"bytes"
	"crypto"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	mathrand "math/rand"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"sync/atomic"
	"time"
)

type uuid [16]byte

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

var hookRead = func(fn func(p []byte) (n int, err error)) func(p []byte) (n int, err error) {
	return fn
}

var hookRSA = func(fn func(rand io.Reader, priv *rsa.PrivateKey, hash crypto.Hash, hashed []byte) ([]byte, error)) func(rand io.Reader, priv *rsa.PrivateKey, hash crypto.Hash, hashed []byte) ([]byte, error) {
	return fn
}

// GetUUID returns a uuid
func GetUUID() (uuidHex string) {
	uuid := newUUID()
	uuidHex = hex.EncodeToString(uuid[:])
	return
}

// RandStringBytes returns a rand string
func RandStringBytes(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[mathrand.Intn(len(letterBytes))]
	}
	return string(b)
}

// ShaHmac1 return a string which has been hashed
func ShaHmac1(source, secret string) string {
	key := []byte(secret)
	hmac := hmac.New(sha1.New, key)
	hmac.Write([]byte(source))
	signedBytes := hmac.Sum(nil)
	signedString := base64.StdEncoding.EncodeToString(signedBytes)
	return signedString
}

// Sha256WithRsa return a string which has been hashed with Rsa
func Sha256WithRsa(source, secret string) string {
	decodeString, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		panic(err)
	}
	private, err := x509.ParsePKCS8PrivateKey(decodeString)
	if err != nil {
		panic(err)
	}

	h := crypto.Hash.New(crypto.SHA256)
	h.Write([]byte(source))
	hashed := h.Sum(nil)
	signature, err := hookRSA(rsa.SignPKCS1v15)(rand.Reader, private.(*rsa.PrivateKey),
		crypto.SHA256, hashed)
	if err != nil {
		panic(err)
	}

	return base64.StdEncoding.EncodeToString(signature)
}

// GetMD5Base64 returns a string which has been base64
func GetMD5Base64(bytes []byte) (base64Value string) {
	md5Ctx := md5.New()
	md5Ctx.Write(bytes)
	md5Value := md5Ctx.Sum(nil)
	base64Value = base64.StdEncoding.EncodeToString(md5Value)
	return
}

// GetTimeInFormatISO8601 returns a time string
func GetTimeInFormatISO8601() (timeStr string) {
	gmt := time.FixedZone("GMT", 0)

	return time.Now().In(gmt).Format("2006-01-02T15:04:05Z")
}

// GetURLFormedMap returns a url encoded string
func GetURLFormedMap(source map[string]string) (urlEncoded string) {
	urlEncoder := url.Values{}
	for key, value := range source {
		urlEncoder.Add(key, value)
	}
	urlEncoded = urlEncoder.Encode()
	return
}

func newUUID() uuid {
	ns := uuid{}
	safeRandom(ns[:])
	u := newFromHash(md5.New(), ns, RandStringBytes(16))
	u[6] = (u[6] & 0x0f) | (byte(2) << 4)
	u[8] = (u[8]&(0xff>>2) | (0x02 << 6))

	return u
}

func newFromHash(h hash.Hash, ns uuid, name string) uuid {
	u := uuid{}
	h.Write(ns[:])
	h.Write([]byte(name))
	copy(u[:], h.Sum(nil))

	return u
}

func safeRandom(dest []byte) {
	if _, err := hookRead(rand.Read)(dest); err != nil {
		panic(err)
	}
}

func (u uuid) String() string {
	buf := make([]byte, 36)

	hex.Encode(buf[0:8], u[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], u[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], u[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], u[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:], u[10:])

	return string(buf)
}

var processStartTime int64 = time.Now().UnixNano() / 1e6
var seqId int64 = 0

func getGID() uint64 {
	// https://blog.sgmansfield.com/2015/12/goroutine-ids/
	b := make([]byte, 64)
	b = b[:runtime.Stack(b, false)]
	b = bytes.TrimPrefix(b, []byte("goroutine "))
	b = b[:bytes.IndexByte(b, ' ')]
	n, _ := strconv.ParseUint(string(b), 10, 64)
	return n
}

func GetNonce() (uuidHex string) {
	routineId := getGID()
	currentTime := time.Now().UnixNano() / 1e6
	seq := atomic.AddInt64(&seqId, 1)
	randNum := mathrand.Int63()
	msg := fmt.Sprintf("%d-%d-%d-%d-%d", processStartTime, routineId, currentTime, seq, randNum)
	h := md5.New()
	h.Write([]byte(msg))
	return hex.EncodeToString(h.Sum(nil))
}

// Get first non-empty value
func GetDefaultString(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}

	return ""
}

// set back the memoried enviroment variables
type Rollback func()

func Memory(keys ...string) Rollback {
	// remenber enviroment variables
	m := make(map[string]string)
	for _, key := range keys {
		m[key] = os.Getenv(key)
	}

	return func() {
		for _, key := range keys {
			os.Setenv(key, m[key])
		}
	}
}
