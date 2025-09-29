package sarama

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"strings"
	"time"

	"github.com/jcmturner/gofork/encoding/asn1"
	"github.com/jcmturner/gokrb5/v8/asn1tools"
	"github.com/jcmturner/gokrb5/v8/gssapi"
	"github.com/jcmturner/gokrb5/v8/iana/chksumtype"
	"github.com/jcmturner/gokrb5/v8/iana/keyusage"
	"github.com/jcmturner/gokrb5/v8/messages"
	"github.com/jcmturner/gokrb5/v8/types"
)

const (
	TOK_ID_KRB_AP_REQ   = 256
	GSS_API_GENERIC_TAG = 0x60
	KRB5_USER_AUTH      = 1
	KRB5_KEYTAB_AUTH    = 2
	KRB5_CCACHE_AUTH    = 3
	GSS_API_INITIAL     = 1
	GSS_API_VERIFY      = 2
	GSS_API_FINISH      = 3
)

type GSSAPIConfig struct {
	AuthType           int
	KeyTabPath         string
	CCachePath         string
	KerberosConfigPath string
	ServiceName        string
	Username           string
	Password           string
	Realm              string
	DisablePAFXFAST    bool
	BuildSpn           BuildSpnFunc
}

type GSSAPIKerberosAuth struct {
	Config                *GSSAPIConfig
	ticket                messages.Ticket
	encKey                types.EncryptionKey
	NewKerberosClientFunc func(config *GSSAPIConfig) (KerberosClient, error)
	step                  int
}

type KerberosClient interface {
	Login() error
	GetServiceTicket(spn string) (messages.Ticket, types.EncryptionKey, error)
	Domain() string
	CName() types.PrincipalName
	Destroy()
}

type BuildSpnFunc func(serviceName, host string) string

// writePackage appends length in big endian before the payload, and sends it to kafka
func (krbAuth *GSSAPIKerberosAuth) writePackage(broker *Broker, payload []byte) (int, error) {
	length := uint64(len(payload))
	size := length + 4 // 4 byte length header + payload
	if size > math.MaxInt32 {
		return 0, errors.New("payload too large, will overflow int32")
	}
	finalPackage := make([]byte, size)
	copy(finalPackage[4:], payload)
	binary.BigEndian.PutUint32(finalPackage, uint32(length))
	bytes, err := broker.conn.Write(finalPackage)
	if err != nil {
		return bytes, err
	}
	return bytes, nil
}

// readPackage reads payload length (4 bytes) and then reads the payload into []byte
func (krbAuth *GSSAPIKerberosAuth) readPackage(broker *Broker) ([]byte, int, error) {
	bytesRead := 0
	lengthInBytes := make([]byte, 4)
	bytes, err := io.ReadFull(broker.conn, lengthInBytes)
	if err != nil {
		return nil, bytesRead, err
	}
	bytesRead += bytes
	payloadLength := binary.BigEndian.Uint32(lengthInBytes)
	payloadBytes := make([]byte, payloadLength)         // buffer for read..
	bytes, err = io.ReadFull(broker.conn, payloadBytes) // read bytes
	if err != nil {
		return payloadBytes, bytesRead, err
	}
	bytesRead += bytes
	return payloadBytes, bytesRead, nil
}

func (krbAuth *GSSAPIKerberosAuth) newAuthenticatorChecksum() []byte {
	a := make([]byte, 24)
	flags := []int{gssapi.ContextFlagInteg, gssapi.ContextFlagConf}
	binary.LittleEndian.PutUint32(a[:4], 16)
	for _, i := range flags {
		f := binary.LittleEndian.Uint32(a[20:24])
		f |= uint32(i)
		binary.LittleEndian.PutUint32(a[20:24], f)
	}
	return a
}

// Construct Kerberos AP_REQ package, conforming to RFC-4120
// https://tools.ietf.org/html/rfc4120#page-84
func (krbAuth *GSSAPIKerberosAuth) createKrb5Token(
	domain string,
	cname types.PrincipalName,
	ticket messages.Ticket,
	sessionKey types.EncryptionKey,
) ([]byte, error) {
	auth, err := types.NewAuthenticator(domain, cname)
	if err != nil {
		return nil, err
	}
	auth.Cksum = types.Checksum{
		CksumType: chksumtype.GSSAPI,
		Checksum:  krbAuth.newAuthenticatorChecksum(),
	}
	APReq, err := messages.NewAPReq(
		ticket,
		sessionKey,
		auth,
	)
	if err != nil {
		return nil, err
	}
	aprBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(aprBytes, TOK_ID_KRB_AP_REQ)
	tb, err := APReq.Marshal()
	if err != nil {
		return nil, err
	}
	aprBytes = append(aprBytes, tb...)
	return aprBytes, nil
}

// Append the GSS-API header to the payload, conforming to RFC-2743
// Section 3.1, Mechanism-Independent Token Format
//
// https://tools.ietf.org/html/rfc2743#page-81
//
// GSSAPIHeader + <specific mechanism payload>
func (krbAuth *GSSAPIKerberosAuth) appendGSSAPIHeader(payload []byte) ([]byte, error) {
	oidBytes, err := asn1.Marshal(gssapi.OIDKRB5.OID())
	if err != nil {
		return nil, err
	}
	tkoLengthBytes := asn1tools.MarshalLengthBytes(len(oidBytes) + len(payload))
	GSSHeader := append([]byte{GSS_API_GENERIC_TAG}, tkoLengthBytes...)
	GSSHeader = append(GSSHeader, oidBytes...)
	GSSPackage := append(GSSHeader, payload...)
	return GSSPackage, nil
}

func (krbAuth *GSSAPIKerberosAuth) initSecContext(
	client KerberosClient,
	bytes []byte,
) ([]byte, error) {
	switch krbAuth.step {
	case GSS_API_INITIAL:
		aprBytes, err := krbAuth.createKrb5Token(
			client.Domain(),
			client.CName(),
			krbAuth.ticket,
			krbAuth.encKey)
		if err != nil {
			return nil, err
		}
		krbAuth.step = GSS_API_VERIFY
		return krbAuth.appendGSSAPIHeader(aprBytes)
	case GSS_API_VERIFY:
		wrapTokenReq := gssapi.WrapToken{}
		if err := wrapTokenReq.Unmarshal(bytes, true); err != nil {
			return nil, err
		}
		// Validate response.
		isValid, err := wrapTokenReq.Verify(krbAuth.encKey, keyusage.GSSAPI_ACCEPTOR_SEAL)
		if !isValid {
			return nil, err
		}

		wrapTokenResponse, err := gssapi.NewInitiatorWrapToken(wrapTokenReq.Payload, krbAuth.encKey)
		if err != nil {
			return nil, err
		}
		krbAuth.step = GSS_API_FINISH
		return wrapTokenResponse.Marshal()
	}
	return nil, nil
}

func (krbAuth *GSSAPIKerberosAuth) spn(broker *Broker) string {
	host, _, _ := net.SplitHostPort(broker.addr)
	var spn string
	if krbAuth.Config.BuildSpn != nil {
		spn = krbAuth.Config.BuildSpn(broker.conf.Net.SASL.GSSAPI.ServiceName, host)
	} else {
		spn = fmt.Sprintf("%s/%s", broker.conf.Net.SASL.GSSAPI.ServiceName, host)
	}
	return spn
}

// Login will use the given KerberosClient to login and get a ticket for the given spn.
func (krbAuth *GSSAPIKerberosAuth) Login(
	client KerberosClient,
	spn string,
) (*messages.Ticket, error) {
	if err := client.Login(); err != nil {
		Logger.Printf("Kerberos client login error: %s", err)
		return nil, err
	}

	ticket, encKey, err := client.GetServiceTicket(spn)
	if err != nil {
		Logger.Printf("Kerberos service ticket error for %s: %s", spn, err)
		return nil, err
	}
	krbAuth.ticket = ticket
	krbAuth.encKey = encKey
	krbAuth.step = GSS_API_INITIAL

	return &ticket, nil
}

// Authorize performs the kerberos auth handshake for authorization
func (krbAuth *GSSAPIKerberosAuth) Authorize(broker *Broker) error {
	client, err := krbAuth.NewKerberosClientFunc(krbAuth.Config)
	if err != nil {
		Logger.Printf("Kerberos client initialization error: %s", err)
		return err
	}
	defer client.Destroy()

	ticket, err := krbAuth.Login(client, krbAuth.spn(broker))
	if err != nil {
		return err
	}

	principal := strings.Join(ticket.SName.NameString, "/") + "@" + ticket.Realm
	var receivedBytes []byte

	for {
		packBytes, err := krbAuth.initSecContext(client, receivedBytes)
		if err != nil {
			Logger.Printf("Kerberos init error as %s: %s", principal, err)
			return err
		}

		requestTime := time.Now()
		bytesWritten, err := krbAuth.writePackage(broker, packBytes)
		if err != nil {
			Logger.Printf("Kerberos write error as %s: %s", principal, err)
			return err
		}
		broker.updateOutgoingCommunicationMetrics(bytesWritten)

		switch krbAuth.step {
		case GSS_API_VERIFY:
			var bytesRead int
			receivedBytes, bytesRead, err = krbAuth.readPackage(broker)
			requestLatency := time.Since(requestTime)
			broker.updateIncomingCommunicationMetrics(bytesRead, requestLatency)
			if err != nil {
				Logger.Printf("Kerberos read error as %s: %s", principal, err)
				return err
			}
		case GSS_API_FINISH:
			return nil
		}
	}
}

// AuthorizeV2 performs the SASL v2 GSSAPI authentication with the Kafka broker.
func (krbAuth *GSSAPIKerberosAuth) AuthorizeV2(
	broker *Broker,
	authSendReceiver func(authBytes []byte) (*SaslAuthenticateResponse, error),
) error {
	client, err := krbAuth.NewKerberosClientFunc(krbAuth.Config)
	if err != nil {
		Logger.Printf("Kerberos client initialization error: %s", err)
		return err
	}
	defer client.Destroy()

	ticket, err := krbAuth.Login(client, krbAuth.spn(broker))
	if err != nil {
		return err
	}

	principal := strings.Join(ticket.SName.NameString, "/") + "@" + ticket.Realm
	var receivedBytes []byte

	for {
		token, err := krbAuth.initSecContext(client, receivedBytes)
		if err != nil {
			Logger.Printf("SASL Kerberos init error as %s: %s", principal, err)
			return err
		}

		authResponse, err := authSendReceiver(token)
		if err != nil {
			Logger.Printf("SASL Kerberos authenticate error as %s: %s", principal, err)
			return err
		}

		receivedBytes = authResponse.SaslAuthBytes

		if krbAuth.step == GSS_API_FINISH {
			return nil
		}
	}
}
