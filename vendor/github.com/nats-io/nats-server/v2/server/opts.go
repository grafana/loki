// Copyright 2012-2022 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"

	"github.com/nats-io/nats-server/v2/conf"
)

var allowUnknownTopLevelField = int32(0)

// NoErrOnUnknownFields can be used to change the behavior the processing
// of a configuration file. By default, an error is reported if unknown
// fields are found. If `noError` is set to true, no error will be reported
// if top-level unknown fields are found.
func NoErrOnUnknownFields(noError bool) {
	var val int32
	if noError {
		val = int32(1)
	}
	atomic.StoreInt32(&allowUnknownTopLevelField, val)
}

// Set of lower case hex-encoded sha256 of DER encoded SubjectPublicKeyInfo
type PinnedCertSet map[string]struct{}

// ClusterOpts are options for clusters.
// NOTE: This structure is no longer used for monitoring endpoints
// and json tags are deprecated and may be removed in the future.
type ClusterOpts struct {
	Name              string            `json:"-"`
	Host              string            `json:"addr,omitempty"`
	Port              int               `json:"cluster_port,omitempty"`
	Username          string            `json:"-"`
	Password          string            `json:"-"`
	AuthTimeout       float64           `json:"auth_timeout,omitempty"`
	Permissions       *RoutePermissions `json:"-"`
	TLSTimeout        float64           `json:"-"`
	TLSConfig         *tls.Config       `json:"-"`
	TLSMap            bool              `json:"-"`
	TLSCheckKnownURLs bool              `json:"-"`
	TLSPinnedCerts    PinnedCertSet     `json:"-"`
	ListenStr         string            `json:"-"`
	Advertise         string            `json:"-"`
	NoAdvertise       bool              `json:"-"`
	ConnectRetries    int               `json:"-"`

	// Not exported (used in tests)
	resolver netResolver
	// Snapshot of configured TLS options.
	tlsConfigOpts *TLSConfigOpts
}

// GatewayOpts are options for gateways.
// NOTE: This structure is no longer used for monitoring endpoints
// and json tags are deprecated and may be removed in the future.
type GatewayOpts struct {
	Name              string               `json:"name"`
	Host              string               `json:"addr,omitempty"`
	Port              int                  `json:"port,omitempty"`
	Username          string               `json:"-"`
	Password          string               `json:"-"`
	AuthTimeout       float64              `json:"auth_timeout,omitempty"`
	TLSConfig         *tls.Config          `json:"-"`
	TLSTimeout        float64              `json:"tls_timeout,omitempty"`
	TLSMap            bool                 `json:"-"`
	TLSCheckKnownURLs bool                 `json:"-"`
	TLSPinnedCerts    PinnedCertSet        `json:"-"`
	Advertise         string               `json:"advertise,omitempty"`
	ConnectRetries    int                  `json:"connect_retries,omitempty"`
	Gateways          []*RemoteGatewayOpts `json:"gateways,omitempty"`
	RejectUnknown     bool                 `json:"reject_unknown,omitempty"` // config got renamed to reject_unknown_cluster

	// Not exported, for tests.
	resolver         netResolver
	sendQSubsBufSize int

	// Snapshot of configured TLS options.
	tlsConfigOpts *TLSConfigOpts
}

// RemoteGatewayOpts are options for connecting to a remote gateway
// NOTE: This structure is no longer used for monitoring endpoints
// and json tags are deprecated and may be removed in the future.
type RemoteGatewayOpts struct {
	Name          string      `json:"name"`
	TLSConfig     *tls.Config `json:"-"`
	TLSTimeout    float64     `json:"tls_timeout,omitempty"`
	URLs          []*url.URL  `json:"urls,omitempty"`
	tlsConfigOpts *TLSConfigOpts
}

// LeafNodeOpts are options for a given server to accept leaf node connections and/or connect to a remote cluster.
type LeafNodeOpts struct {
	Host              string        `json:"addr,omitempty"`
	Port              int           `json:"port,omitempty"`
	Username          string        `json:"-"`
	Password          string        `json:"-"`
	Account           string        `json:"-"`
	Users             []*User       `json:"-"`
	AuthTimeout       float64       `json:"auth_timeout,omitempty"`
	TLSConfig         *tls.Config   `json:"-"`
	TLSTimeout        float64       `json:"tls_timeout,omitempty"`
	TLSMap            bool          `json:"-"`
	TLSPinnedCerts    PinnedCertSet `json:"-"`
	Advertise         string        `json:"-"`
	NoAdvertise       bool          `json:"-"`
	ReconnectInterval time.Duration `json:"-"`

	// For solicited connections to other clusters/superclusters.
	Remotes []*RemoteLeafOpts `json:"remotes,omitempty"`

	// This is the minimum version that is accepted for remote connections.
	// Note that since the server version in the CONNECT protocol was added
	// only starting at v2.8.0, any version below that will be rejected
	// (since empty version string in CONNECT would fail the "version at
	// least" test).
	MinVersion string

	// Not exported, for tests.
	resolver    netResolver
	dialTimeout time.Duration
	connDelay   time.Duration

	// Snapshot of configured TLS options.
	tlsConfigOpts *TLSConfigOpts
}

// SignatureHandler is used to sign a nonce from the server while
// authenticating with Nkeys. The callback should sign the nonce and
// return the JWT and the raw signature.
type SignatureHandler func([]byte) (string, []byte, error)

// RemoteLeafOpts are options for connecting to a remote server as a leaf node.
type RemoteLeafOpts struct {
	LocalAccount string           `json:"local_account,omitempty"`
	NoRandomize  bool             `json:"-"`
	URLs         []*url.URL       `json:"urls,omitempty"`
	Credentials  string           `json:"-"`
	SignatureCB  SignatureHandler `json:"-"`
	TLS          bool             `json:"-"`
	TLSConfig    *tls.Config      `json:"-"`
	TLSTimeout   float64          `json:"tls_timeout,omitempty"`
	Hub          bool             `json:"hub,omitempty"`
	DenyImports  []string         `json:"-"`
	DenyExports  []string         `json:"-"`

	// When an URL has the "ws" (or "wss") scheme, then the server will initiate the
	// connection as a websocket connection. By default, the websocket frames will be
	// masked (as if this server was a websocket client to the remote server). The
	// NoMasking option will change this behavior and will send umasked frames.
	Websocket struct {
		Compression bool `json:"-"`
		NoMasking   bool `json:"-"`
	}

	tlsConfigOpts *TLSConfigOpts

	// If we are clustered and our local account has JetStream, if apps are accessing
	// a stream or consumer leader through this LN and it gets dropped, the apps will
	// not be able to work. This tells the system to migrate the leaders away from this server.
	// This only changes leader for R>1 assets.
	JetStreamClusterMigrate bool `json:"jetstream_cluster_migrate,omitempty"`
}

type JSLimitOpts struct {
	MaxRequestBatch int
	MaxAckPending   int
	MaxHAAssets     int
	Duplicates      time.Duration
}

// Options block for nats-server.
// NOTE: This structure is no longer used for monitoring endpoints
// and json tags are deprecated and may be removed in the future.
type Options struct {
	ConfigFile            string        `json:"-"`
	ServerName            string        `json:"server_name"`
	Host                  string        `json:"addr"`
	Port                  int           `json:"port"`
	DontListen            bool          `json:"dont_listen"`
	ClientAdvertise       string        `json:"-"`
	Trace                 bool          `json:"-"`
	Debug                 bool          `json:"-"`
	TraceVerbose          bool          `json:"-"`
	NoLog                 bool          `json:"-"`
	NoSigs                bool          `json:"-"`
	NoSublistCache        bool          `json:"-"`
	NoHeaderSupport       bool          `json:"-"`
	DisableShortFirstPing bool          `json:"-"`
	Logtime               bool          `json:"-"`
	MaxConn               int           `json:"max_connections"`
	MaxSubs               int           `json:"max_subscriptions,omitempty"`
	MaxSubTokens          uint8         `json:"-"`
	Nkeys                 []*NkeyUser   `json:"-"`
	Users                 []*User       `json:"-"`
	Accounts              []*Account    `json:"-"`
	NoAuthUser            string        `json:"-"`
	SystemAccount         string        `json:"-"`
	NoSystemAccount       bool          `json:"-"`
	Username              string        `json:"-"`
	Password              string        `json:"-"`
	Authorization         string        `json:"-"`
	PingInterval          time.Duration `json:"ping_interval"`
	MaxPingsOut           int           `json:"ping_max"`
	HTTPHost              string        `json:"http_host"`
	HTTPPort              int           `json:"http_port"`
	HTTPBasePath          string        `json:"http_base_path"`
	HTTPSPort             int           `json:"https_port"`
	AuthTimeout           float64       `json:"auth_timeout"`
	MaxControlLine        int32         `json:"max_control_line"`
	MaxPayload            int32         `json:"max_payload"`
	MaxPending            int64         `json:"max_pending"`
	Cluster               ClusterOpts   `json:"cluster,omitempty"`
	Gateway               GatewayOpts   `json:"gateway,omitempty"`
	LeafNode              LeafNodeOpts  `json:"leaf,omitempty"`
	JetStream             bool          `json:"jetstream"`
	JetStreamMaxMemory    int64         `json:"-"`
	JetStreamMaxStore     int64         `json:"-"`
	JetStreamDomain       string        `json:"-"`
	JetStreamExtHint      string        `json:"-"`
	JetStreamKey          string        `json:"-"`
	JetStreamCipher       StoreCipher   `json:"-"`
	JetStreamUniqueTag    string
	JetStreamLimits       JSLimitOpts
	JetStreamMaxCatchup   int64
	StoreDir              string            `json:"-"`
	JsAccDefaultDomain    map[string]string `json:"-"` // account to domain name mapping
	Websocket             WebsocketOpts     `json:"-"`
	MQTT                  MQTTOpts          `json:"-"`
	ProfPort              int               `json:"-"`
	PidFile               string            `json:"-"`
	PortsFileDir          string            `json:"-"`
	LogFile               string            `json:"-"`
	LogSizeLimit          int64             `json:"-"`
	Syslog                bool              `json:"-"`
	RemoteSyslog          string            `json:"-"`
	Routes                []*url.URL        `json:"-"`
	RoutesStr             string            `json:"-"`
	TLSTimeout            float64           `json:"tls_timeout"`
	TLS                   bool              `json:"-"`
	TLSVerify             bool              `json:"-"`
	TLSMap                bool              `json:"-"`
	TLSCert               string            `json:"-"`
	TLSKey                string            `json:"-"`
	TLSCaCert             string            `json:"-"`
	TLSConfig             *tls.Config       `json:"-"`
	TLSPinnedCerts        PinnedCertSet     `json:"-"`
	TLSRateLimit          int64             `json:"-"`
	AllowNonTLS           bool              `json:"-"`
	WriteDeadline         time.Duration     `json:"-"`
	MaxClosedClients      int               `json:"-"`
	LameDuckDuration      time.Duration     `json:"-"`
	LameDuckGracePeriod   time.Duration     `json:"-"`

	// MaxTracedMsgLen is the maximum printable length for traced messages.
	MaxTracedMsgLen int `json:"-"`

	// Operating a trusted NATS server
	TrustedKeys              []string              `json:"-"`
	TrustedOperators         []*jwt.OperatorClaims `json:"-"`
	AccountResolver          AccountResolver       `json:"-"`
	AccountResolverTLSConfig *tls.Config           `json:"-"`

	// AlwaysEnableNonce will always present a nonce to new connections
	// typically used by custom Authentication implementations who embeds
	// the server and so not presented as a configuration option
	AlwaysEnableNonce bool

	CustomClientAuthentication Authentication `json:"-"`
	CustomRouterAuthentication Authentication `json:"-"`

	// CheckConfig configuration file syntax test was successful and exit.
	CheckConfig bool `json:"-"`

	// ConnectErrorReports specifies the number of failed attempts
	// at which point server should report the failure of an initial
	// connection to a route, gateway or leaf node.
	// See DEFAULT_CONNECT_ERROR_REPORTS for default value.
	ConnectErrorReports int

	// ReconnectErrorReports is similar to ConnectErrorReports except
	// that this applies to reconnect events.
	ReconnectErrorReports int

	// Tags describing the server. They will be included in varz
	// and used as a filter criteria for some system requests.
	Tags jwt.TagList `json:"-"`

	// OCSPConfig enables OCSP Stapling in the server.
	OCSPConfig    *OCSPConfig
	tlsConfigOpts *TLSConfigOpts

	// private fields, used to know if bool options are explicitly
	// defined in config and/or command line params.
	inConfig  map[string]bool
	inCmdLine map[string]bool

	// private fields for operator mode
	operatorJWT            []string
	resolverPreloads       map[string]string
	resolverPinnedAccounts map[string]struct{}

	// private fields, used for testing
	gatewaysSolicitDelay time.Duration
	routeProto           int

	// JetStream
	maxMemSet   bool
	maxStoreSet bool
}

// WebsocketOpts are options for websocket
type WebsocketOpts struct {
	// The server will accept websocket client connections on this hostname/IP.
	Host string
	// The server will accept websocket client connections on this port.
	Port int
	// The host:port to advertise to websocket clients in the cluster.
	Advertise string

	// If no user name is provided when a client connects, will default to the
	// matching user from the global list of users in `Options.Users`.
	NoAuthUser string

	// Name of the cookie, which if present in WebSocket upgrade headers,
	// will be treated as JWT during CONNECT phase as long as
	// "jwt" specified in the CONNECT options is missing or empty.
	JWTCookie string

	// Authentication section. If anything is configured in this section,
	// it will override the authorization configuration of regular clients.
	Username string
	Password string
	Token    string

	// Timeout for the authentication process.
	AuthTimeout float64

	// By default the server will enforce the use of TLS. If no TLS configuration
	// is provided, you need to explicitly set NoTLS to true to allow the server
	// to start without TLS configuration. Note that if a TLS configuration is
	// present, this boolean is ignored and the server will run the Websocket
	// server with that TLS configuration.
	// Running without TLS is less secure since Websocket clients that use bearer
	// tokens will send them in clear. So this should not be used in production.
	NoTLS bool

	// TLS configuration is required.
	TLSConfig *tls.Config
	// If true, map certificate values for authentication purposes.
	TLSMap bool

	// When present, accepted client certificates (verify/verify_and_map) must be in this list
	TLSPinnedCerts PinnedCertSet

	// If true, the Origin header must match the request's host.
	SameOrigin bool

	// Only origins in this list will be accepted. If empty and
	// SameOrigin is false, any origin is accepted.
	AllowedOrigins []string

	// If set to true, the server will negotiate with clients
	// if compression can be used. If this is false, no compression
	// will be used (both in server and clients) since it has to
	// be negotiated between both endpoints
	Compression bool

	// Total time allowed for the server to read the client request
	// and write the response back to the client. This include the
	// time needed for the TLS Handshake.
	HandshakeTimeout time.Duration
}

// MQTTOpts are options for MQTT
type MQTTOpts struct {
	// The server will accept MQTT client connections on this hostname/IP.
	Host string
	// The server will accept MQTT client connections on this port.
	Port int

	// If no user name is provided when a client connects, will default to the
	// matching user from the global list of users in `Options.Users`.
	NoAuthUser string

	// Authentication section. If anything is configured in this section,
	// it will override the authorization configuration of regular clients.
	Username string
	Password string
	Token    string

	// JetStream domain mqtt is supposed to pick up
	JsDomain string

	// Number of replicas for MQTT streams.
	// Negative or 0 value means that the server(s) will pick a replica
	// number based on the known size of the cluster (but capped at 3).
	// Note that if an account was already connected, the stream's replica
	// count is not modified. Use the NATS CLI to update the count if desired.
	StreamReplicas int

	// Number of replicas for MQTT consumers.
	// Negative or 0 value means that there is no override and the consumer
	// will have the same replica factor that the stream it belongs to.
	// If a value is specified, it will require to be lower than the stream
	// replicas count (lower than StreamReplicas if specified, but also lower
	// than the automatic value determined by cluster size).
	// Note that existing consumers are not modified.
	//
	// UPDATE: This is no longer used while messages stream has interest policy retention
	// which requires consumer replica count to match the parent stream.
	ConsumerReplicas int

	// Indicate if the consumers should be created with memory storage.
	// Note that existing consumers are not modified.
	ConsumerMemoryStorage bool

	// If specified will have the system auto-cleanup the consumers after being
	// inactive for the specified amount of time.
	ConsumerInactiveThreshold time.Duration

	// Timeout for the authentication process.
	AuthTimeout float64

	// TLS configuration is required.
	TLSConfig *tls.Config
	// If true, map certificate values for authentication purposes.
	TLSMap bool
	// Timeout for the TLS handshake
	TLSTimeout float64
	// Set of allowable certificates
	TLSPinnedCerts PinnedCertSet

	// AckWait is the amount of time after which a QoS 1 message sent to
	// a client is redelivered as a DUPLICATE if the server has not
	// received the PUBACK on the original Packet Identifier.
	// The value has to be positive.
	// Zero will cause the server to use the default value (30 seconds).
	// Note that changes to this option is applied only to new MQTT subscriptions.
	AckWait time.Duration

	// MaxAckPending is the amount of QoS 1 messages the server can send to
	// a subscription without receiving any PUBACK for those messages.
	// The valid range is [0..65535].
	// The total of subscriptions' MaxAckPending on a given session cannot
	// exceed 65535. Attempting to create a subscription that would bring
	// the total above the limit would result in the server returning 0x80
	// in the SUBACK for this subscription.
	// Due to how the NATS Server handles the MQTT "#" wildcard, each
	// subscription ending with "#" will use 2 times the MaxAckPending value.
	// Note that changes to this option is applied only to new subscriptions.
	MaxAckPending uint16
}

type netResolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
}

// Clone performs a deep copy of the Options struct, returning a new clone
// with all values copied.
func (o *Options) Clone() *Options {
	if o == nil {
		return nil
	}
	clone := &Options{}
	*clone = *o
	if o.Users != nil {
		clone.Users = make([]*User, len(o.Users))
		for i, user := range o.Users {
			clone.Users[i] = user.clone()
		}
	}
	if o.Nkeys != nil {
		clone.Nkeys = make([]*NkeyUser, len(o.Nkeys))
		for i, nkey := range o.Nkeys {
			clone.Nkeys[i] = nkey.clone()
		}
	}

	if o.Routes != nil {
		clone.Routes = deepCopyURLs(o.Routes)
	}
	if o.TLSConfig != nil {
		clone.TLSConfig = o.TLSConfig.Clone()
	}
	if o.Cluster.TLSConfig != nil {
		clone.Cluster.TLSConfig = o.Cluster.TLSConfig.Clone()
	}
	if o.Gateway.TLSConfig != nil {
		clone.Gateway.TLSConfig = o.Gateway.TLSConfig.Clone()
	}
	if len(o.Gateway.Gateways) > 0 {
		clone.Gateway.Gateways = make([]*RemoteGatewayOpts, len(o.Gateway.Gateways))
		for i, g := range o.Gateway.Gateways {
			clone.Gateway.Gateways[i] = g.clone()
		}
	}
	// FIXME(dlc) - clone leaf node stuff.
	return clone
}

func deepCopyURLs(urls []*url.URL) []*url.URL {
	if urls == nil {
		return nil
	}
	curls := make([]*url.URL, len(urls))
	for i, u := range urls {
		cu := &url.URL{}
		*cu = *u
		curls[i] = cu
	}
	return curls
}

// Configuration file authorization section.
type authorization struct {
	// Singles
	user  string
	pass  string
	token string
	acc   string
	// Multiple Nkeys/Users
	nkeys              []*NkeyUser
	users              []*User
	timeout            float64
	defaultPermissions *Permissions
}

// TLSConfigOpts holds the parsed tls config information,
// used with flag parsing
type TLSConfigOpts struct {
	CertFile          string
	KeyFile           string
	CaFile            string
	Verify            bool
	Insecure          bool
	Map               bool
	TLSCheckKnownURLs bool
	Timeout           float64
	RateLimit         int64
	Ciphers           []uint16
	CurvePreferences  []tls.CurveID
	PinnedCerts       PinnedCertSet
}

// OCSPConfig represents the options of OCSP stapling options.
type OCSPConfig struct {
	// Mode defines the policy for OCSP stapling.
	Mode OCSPMode

	// OverrideURLs is the http URL endpoint used to get OCSP staples.
	OverrideURLs []string
}

var tlsUsage = `
TLS configuration is specified in the tls section of a configuration file:

e.g.

    tls {
        cert_file:      "./certs/server-cert.pem"
        key_file:       "./certs/server-key.pem"
        ca_file:        "./certs/ca.pem"
        verify:         true
        verify_and_map: true

        cipher_suites: [
            "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
            "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"
        ]
        curve_preferences: [
            "CurveP256",
            "CurveP384",
            "CurveP521"
        ]
    }

Available cipher suites include:
`

// ProcessConfigFile processes a configuration file.
// FIXME(dlc): A bit hacky
func ProcessConfigFile(configFile string) (*Options, error) {
	opts := &Options{}
	if err := opts.ProcessConfigFile(configFile); err != nil {
		// If only warnings then continue and return the options.
		if cerr, ok := err.(*processConfigErr); ok && len(cerr.Errors()) == 0 {
			return opts, nil
		}

		return nil, err
	}
	return opts, nil
}

// token is an item parsed from the configuration.
type token interface {
	Value() interface{}
	Line() int
	IsUsedVariable() bool
	SourceFile() string
	Position() int
}

// unwrapValue can be used to get the token and value from an item
// to be able to report the line number in case of an incorrect
// configuration.
// also stores the token in lastToken for use in convertPanicToError
func unwrapValue(v interface{}, lastToken *token) (token, interface{}) {
	switch tk := v.(type) {
	case token:
		if lastToken != nil {
			*lastToken = tk
		}
		return tk, tk.Value()
	default:
		return nil, v
	}
}

// use in defer to recover from panic and turn it into an error associated with last token
func convertPanicToErrorList(lastToken *token, errors *[]error) {
	// only recover if an error can be stored
	if errors == nil {
		return
	} else if err := recover(); err == nil {
		return
	} else if lastToken != nil && *lastToken != nil {
		*errors = append(*errors, &configErr{*lastToken, fmt.Sprint(err)})
	} else {
		*errors = append(*errors, fmt.Errorf("encountered panic without a token %v", err))
	}
}

// use in defer to recover from panic and turn it into an error associated with last token
func convertPanicToError(lastToken *token, e *error) {
	// only recover if an error can be stored
	if e == nil || *e != nil {
		return
	} else if err := recover(); err == nil {
		return
	} else if lastToken != nil && *lastToken != nil {
		*e = &configErr{*lastToken, fmt.Sprint(err)}
	} else {
		*e = fmt.Errorf("%v", err)
	}
}

// configureSystemAccount configures a system account
// if present in the configuration.
func configureSystemAccount(o *Options, m map[string]interface{}) (retErr error) {
	var lt token
	defer convertPanicToError(&lt, &retErr)
	configure := func(v interface{}) error {
		tk, v := unwrapValue(v, &lt)
		sa, ok := v.(string)
		if !ok {
			return &configErr{tk, "system account name must be a string"}
		}
		o.SystemAccount = sa
		return nil
	}

	if v, ok := m["system_account"]; ok {
		return configure(v)
	} else if v, ok := m["system"]; ok {
		return configure(v)
	}

	return nil
}

// ProcessConfigFile updates the Options structure with options
// present in the given configuration file.
// This version is convenient if one wants to set some default
// options and then override them with what is in the config file.
// For instance, this version allows you to do something such as:
//
// opts := &Options{Debug: true}
// opts.ProcessConfigFile(myConfigFile)
//
// If the config file contains "debug: false", after this call,
// opts.Debug would really be false. It would be impossible to
// achieve that with the non receiver ProcessConfigFile() version,
// since one would not know after the call if "debug" was not present
// or was present but set to false.
func (o *Options) ProcessConfigFile(configFile string) error {
	o.ConfigFile = configFile
	if configFile == _EMPTY_ {
		return nil
	}
	m, err := conf.ParseFileWithChecks(configFile)
	if err != nil {
		return err
	}
	// Collect all errors and warnings and report them all together.
	errors := make([]error, 0)
	warnings := make([]error, 0)

	// First check whether a system account has been defined,
	// as that is a condition for other features to be enabled.
	if err := configureSystemAccount(o, m); err != nil {
		errors = append(errors, err)
	}

	for k, v := range m {
		o.processConfigFileLine(k, v, &errors, &warnings)
	}

	if len(errors) > 0 || len(warnings) > 0 {
		return &processConfigErr{
			errors:   errors,
			warnings: warnings,
		}
	}

	return nil
}

func (o *Options) processConfigFileLine(k string, v interface{}, errors *[]error, warnings *[]error) {
	var lt token
	defer convertPanicToErrorList(&lt, errors)

	tk, v := unwrapValue(v, &lt)
	switch strings.ToLower(k) {
	case "listen":
		hp, err := parseListen(v)
		if err != nil {
			*errors = append(*errors, &configErr{tk, err.Error()})
			return
		}
		o.Host = hp.host
		o.Port = hp.port
	case "client_advertise":
		o.ClientAdvertise = v.(string)
	case "port":
		o.Port = int(v.(int64))
	case "server_name":
		o.ServerName = v.(string)
	case "host", "net":
		o.Host = v.(string)
	case "debug":
		o.Debug = v.(bool)
		trackExplicitVal(o, &o.inConfig, "Debug", o.Debug)
	case "trace":
		o.Trace = v.(bool)
		trackExplicitVal(o, &o.inConfig, "Trace", o.Trace)
	case "trace_verbose":
		o.TraceVerbose = v.(bool)
		o.Trace = v.(bool)
		trackExplicitVal(o, &o.inConfig, "TraceVerbose", o.TraceVerbose)
		trackExplicitVal(o, &o.inConfig, "Trace", o.Trace)
	case "logtime":
		o.Logtime = v.(bool)
		trackExplicitVal(o, &o.inConfig, "Logtime", o.Logtime)
	case "mappings", "maps":
		gacc := NewAccount(globalAccountName)
		o.Accounts = append(o.Accounts, gacc)
		err := parseAccountMappings(tk, gacc, errors, warnings)
		if err != nil {
			*errors = append(*errors, err)
			return
		}
	case "disable_sublist_cache", "no_sublist_cache":
		o.NoSublistCache = v.(bool)
	case "accounts":
		err := parseAccounts(tk, o, errors, warnings)
		if err != nil {
			*errors = append(*errors, err)
			return
		}
	case "authorization":
		auth, err := parseAuthorization(tk, o, errors, warnings)
		if err != nil {
			*errors = append(*errors, err)
			return
		}

		o.Username = auth.user
		o.Password = auth.pass
		o.Authorization = auth.token
		o.AuthTimeout = auth.timeout
		if (auth.user != _EMPTY_ || auth.pass != _EMPTY_) && auth.token != _EMPTY_ {
			err := &configErr{tk, "Cannot have a user/pass and token"}
			*errors = append(*errors, err)
			return
		}
		// In case parseAccounts() was done first, we need to check for duplicates.
		unames := setupUsersAndNKeysDuplicateCheckMap(o)
		// Check for multiple users defined.
		// Note: auth.users will be != nil as long as `users: []` is present
		// in the authorization block, even if empty, and will also account for
		// nkey users. We also check for users/nkeys that may have been already
		// added in parseAccounts() (which means they will be in unames)
		if auth.users != nil || len(unames) > 0 {
			if auth.user != _EMPTY_ {
				err := &configErr{tk, "Can not have a single user/pass and a users array"}
				*errors = append(*errors, err)
				return
			}
			if auth.token != _EMPTY_ {
				err := &configErr{tk, "Can not have a token and a users array"}
				*errors = append(*errors, err)
				return
			}
			// Now check that if we have users, there is no duplicate, including
			// users that may have been configured in parseAccounts().
			if len(auth.users) > 0 {
				for _, u := range auth.users {
					if _, ok := unames[u.Username]; ok {
						err := &configErr{tk, fmt.Sprintf("Duplicate user %q detected", u.Username)}
						*errors = append(*errors, err)
						return
					}
					unames[u.Username] = struct{}{}
				}
				// Users may have been added from Accounts parsing, so do an append here
				o.Users = append(o.Users, auth.users...)
			}
		}
		// Check for nkeys
		if len(auth.nkeys) > 0 {
			for _, u := range auth.nkeys {
				if _, ok := unames[u.Nkey]; ok {
					err := &configErr{tk, fmt.Sprintf("Duplicate nkey %q detected", u.Nkey)}
					*errors = append(*errors, err)
					return
				}
				unames[u.Nkey] = struct{}{}
			}
			// NKeys may have been added from Accounts parsing, so do an append here
			o.Nkeys = append(o.Nkeys, auth.nkeys...)
		}
	case "http":
		hp, err := parseListen(v)
		if err != nil {
			err := &configErr{tk, err.Error()}
			*errors = append(*errors, err)
			return
		}
		o.HTTPHost = hp.host
		o.HTTPPort = hp.port
	case "https":
		hp, err := parseListen(v)
		if err != nil {
			err := &configErr{tk, err.Error()}
			*errors = append(*errors, err)
			return
		}
		o.HTTPHost = hp.host
		o.HTTPSPort = hp.port
	case "http_port", "monitor_port":
		o.HTTPPort = int(v.(int64))
	case "https_port":
		o.HTTPSPort = int(v.(int64))
	case "http_base_path":
		o.HTTPBasePath = v.(string)
	case "cluster":
		err := parseCluster(tk, o, errors, warnings)
		if err != nil {
			*errors = append(*errors, err)
			return
		}
	case "gateway":
		if err := parseGateway(tk, o, errors, warnings); err != nil {
			*errors = append(*errors, err)
			return
		}
	case "leaf", "leafnodes":
		err := parseLeafNodes(tk, o, errors, warnings)
		if err != nil {
			*errors = append(*errors, err)
			return
		}
	case "store_dir", "storedir":
		// Check if JetStream configuration is also setting the storage directory.
		if o.StoreDir != "" {
			*errors = append(*errors, &configErr{tk, "Duplicate 'store_dir' configuration"})
			return
		}
		o.StoreDir = v.(string)
	case "jetstream":
		err := parseJetStream(tk, o, errors, warnings)
		if err != nil {
			*errors = append(*errors, err)
			return
		}
	case "logfile", "log_file":
		o.LogFile = v.(string)
	case "logfile_size_limit", "log_size_limit":
		o.LogSizeLimit = v.(int64)
	case "syslog":
		o.Syslog = v.(bool)
		trackExplicitVal(o, &o.inConfig, "Syslog", o.Syslog)
	case "remote_syslog":
		o.RemoteSyslog = v.(string)
	case "pidfile", "pid_file":
		o.PidFile = v.(string)
	case "ports_file_dir":
		o.PortsFileDir = v.(string)
	case "prof_port":
		o.ProfPort = int(v.(int64))
	case "max_control_line":
		if v.(int64) > 1<<31-1 {
			err := &configErr{tk, fmt.Sprintf("%s value is too big", k)}
			*errors = append(*errors, err)
			return
		}
		o.MaxControlLine = int32(v.(int64))
	case "max_payload":
		if v.(int64) > 1<<31-1 {
			err := &configErr{tk, fmt.Sprintf("%s value is too big", k)}
			*errors = append(*errors, err)
			return
		}
		o.MaxPayload = int32(v.(int64))
	case "max_pending":
		o.MaxPending = v.(int64)
	case "max_connections", "max_conn":
		o.MaxConn = int(v.(int64))
	case "max_traced_msg_len":
		o.MaxTracedMsgLen = int(v.(int64))
	case "max_subscriptions", "max_subs":
		o.MaxSubs = int(v.(int64))
	case "max_sub_tokens", "max_subscription_tokens":
		if n := v.(int64); n > math.MaxUint8 {
			err := &configErr{tk, fmt.Sprintf("%s value is too big", k)}
			*errors = append(*errors, err)
			return
		} else if n <= 0 {
			err := &configErr{tk, fmt.Sprintf("%s value can not be negative", k)}
			*errors = append(*errors, err)
			return
		} else {
			o.MaxSubTokens = uint8(n)
		}
	case "ping_interval":
		o.PingInterval = parseDuration("ping_interval", tk, v, errors, warnings)
	case "ping_max":
		o.MaxPingsOut = int(v.(int64))
	case "tls":
		tc, err := parseTLS(tk, true)
		if err != nil {
			*errors = append(*errors, err)
			return
		}
		if o.TLSConfig, err = GenTLSConfig(tc); err != nil {
			err := &configErr{tk, err.Error()}
			*errors = append(*errors, err)
			return
		}
		o.TLSTimeout = tc.Timeout
		o.TLSMap = tc.Map
		o.TLSPinnedCerts = tc.PinnedCerts
		o.TLSRateLimit = tc.RateLimit

		// Need to keep track of path of the original TLS config
		// and certs path for OCSP Stapling monitoring.
		o.tlsConfigOpts = tc
	case "ocsp":
		switch vv := v.(type) {
		case bool:
			if vv {
				// Default is Auto which honors Must Staple status request
				// but does not shutdown the server in case it is revoked,
				// letting the client choose whether to trust or not the server.
				o.OCSPConfig = &OCSPConfig{Mode: OCSPModeAuto}
			} else {
				o.OCSPConfig = &OCSPConfig{Mode: OCSPModeNever}
			}
		case map[string]interface{}:
			ocsp := &OCSPConfig{Mode: OCSPModeAuto}

			for kk, kv := range vv {
				_, v = unwrapValue(kv, &tk)
				switch kk {
				case "mode":
					mode := v.(string)
					switch {
					case strings.EqualFold(mode, "always"):
						ocsp.Mode = OCSPModeAlways
					case strings.EqualFold(mode, "must"):
						ocsp.Mode = OCSPModeMust
					case strings.EqualFold(mode, "never"):
						ocsp.Mode = OCSPModeNever
					case strings.EqualFold(mode, "auto"):
						ocsp.Mode = OCSPModeAuto
					default:
						*errors = append(*errors, &configErr{tk, fmt.Sprintf("error parsing ocsp config: unsupported ocsp mode %T", mode)})
					}
				case "urls":
					urls := v.([]string)
					ocsp.OverrideURLs = urls
				case "url":
					url := v.(string)
					ocsp.OverrideURLs = []string{url}
				default:
					*errors = append(*errors, &configErr{tk, fmt.Sprintf("error parsing ocsp config: unsupported field %T", kk)})
					return
				}
			}
			o.OCSPConfig = ocsp
		default:
			*errors = append(*errors, &configErr{tk, fmt.Sprintf("error parsing ocsp config: unsupported type %T", v)})
			return
		}
	case "allow_non_tls":
		o.AllowNonTLS = v.(bool)
	case "write_deadline":
		o.WriteDeadline = parseDuration("write_deadline", tk, v, errors, warnings)
	case "lame_duck_duration":
		dur, err := time.ParseDuration(v.(string))
		if err != nil {
			err := &configErr{tk, fmt.Sprintf("error parsing lame_duck_duration: %v", err)}
			*errors = append(*errors, err)
			return
		}
		if dur < 30*time.Second {
			err := &configErr{tk, fmt.Sprintf("invalid lame_duck_duration of %v, minimum is 30 seconds", dur)}
			*errors = append(*errors, err)
			return
		}
		o.LameDuckDuration = dur
	case "lame_duck_grace_period":
		dur, err := time.ParseDuration(v.(string))
		if err != nil {
			err := &configErr{tk, fmt.Sprintf("error parsing lame_duck_grace_period: %v", err)}
			*errors = append(*errors, err)
			return
		}
		if dur < 0 {
			err := &configErr{tk, "invalid lame_duck_grace_period, needs to be positive"}
			*errors = append(*errors, err)
			return
		}
		o.LameDuckGracePeriod = dur
	case "operator", "operators", "roots", "root", "root_operators", "root_operator":
		opFiles := []string{}
		switch v := v.(type) {
		case string:
			opFiles = append(opFiles, v)
		case []string:
			opFiles = append(opFiles, v...)
		default:
			err := &configErr{tk, fmt.Sprintf("error parsing operators: unsupported type %T", v)}
			*errors = append(*errors, err)
		}
		// Assume for now these are file names, but they can also be the JWT itself inline.
		o.TrustedOperators = make([]*jwt.OperatorClaims, 0, len(opFiles))
		for _, fname := range opFiles {
			theJWT, opc, err := readOperatorJWT(fname)
			if err != nil {
				err := &configErr{tk, fmt.Sprintf("error parsing operator JWT: %v", err)}
				*errors = append(*errors, err)
				continue
			}
			o.operatorJWT = append(o.operatorJWT, theJWT)
			o.TrustedOperators = append(o.TrustedOperators, opc)
		}
		if len(o.TrustedOperators) == 1 {
			// In case "resolver" is defined as well, it takes precedence
			if o.AccountResolver == nil {
				if accUrl, err := parseURL(o.TrustedOperators[0].AccountServerURL, "account resolver"); err == nil {
					// nsc automatically appends "/accounts" during nsc push
					o.AccountResolver, _ = NewURLAccResolver(accUrl.String() + "/accounts")
				}
			}
			// In case "system_account" is defined as well, it takes precedence
			if o.SystemAccount == _EMPTY_ {
				o.SystemAccount = o.TrustedOperators[0].SystemAccount
			}
		}
	case "resolver", "account_resolver", "accounts_resolver":
		switch v := v.(type) {
		case string:
			// "resolver" takes precedence over value obtained from "operator".
			// Clear so that parsing errors are not silently ignored.
			o.AccountResolver = nil
			memResolverRe := regexp.MustCompile(`(?i)(MEM|MEMORY)\s*`)
			resolverRe := regexp.MustCompile(`(?i)(?:URL){1}(?:\({1}\s*"?([^\s"]*)"?\s*\){1})?\s*`)
			if memResolverRe.MatchString(v) {
				o.AccountResolver = &MemAccResolver{}
			} else if items := resolverRe.FindStringSubmatch(v); len(items) == 2 {
				url := items[1]
				_, err := parseURL(url, "account resolver")
				if err != nil {
					*errors = append(*errors, &configErr{tk, err.Error()})
					return
				}
				if ur, err := NewURLAccResolver(url); err != nil {
					err := &configErr{tk, err.Error()}
					*errors = append(*errors, err)
					return
				} else {
					o.AccountResolver = ur
				}
			}
		case map[string]interface{}:
			del := false
			dir := ""
			dirType := ""
			limit := int64(0)
			ttl := time.Duration(0)
			sync := time.Duration(0)
			opts := []DirResOption{}
			var err error
			if v, ok := v["dir"]; ok {
				_, v := unwrapValue(v, &lt)
				dir = v.(string)
			}
			if v, ok := v["type"]; ok {
				_, v := unwrapValue(v, &lt)
				dirType = v.(string)
			}
			if v, ok := v["allow_delete"]; ok {
				_, v := unwrapValue(v, &lt)
				del = v.(bool)
			}
			if v, ok := v["limit"]; ok {
				_, v := unwrapValue(v, &lt)
				limit = v.(int64)
			}
			if v, ok := v["ttl"]; ok {
				_, v := unwrapValue(v, &lt)
				ttl, err = time.ParseDuration(v.(string))
			}
			if v, ok := v["interval"]; err == nil && ok {
				_, v := unwrapValue(v, &lt)
				sync, err = time.ParseDuration(v.(string))
			}
			if v, ok := v["timeout"]; err == nil && ok {
				_, v := unwrapValue(v, &lt)
				var to time.Duration
				if to, err = time.ParseDuration(v.(string)); err == nil {
					opts = append(opts, FetchTimeout(to))
				}
			}
			if err != nil {
				*errors = append(*errors, &configErr{tk, err.Error()})
				return
			}
			if dir == "" {
				*errors = append(*errors, &configErr{tk, "dir has no value and needs to point to a directory"})
				return
			}
			if info, _ := os.Stat(dir); info != nil && (!info.IsDir() || info.Mode().Perm()&(1<<(uint(7))) == 0) {
				*errors = append(*errors, &configErr{tk, "dir needs to point to an accessible directory"})
				return
			}
			var res AccountResolver
			switch strings.ToUpper(dirType) {
			case "CACHE":
				if sync != 0 {
					*errors = append(*errors, &configErr{tk, "CACHE does not accept sync"})
				}
				if del {
					*errors = append(*errors, &configErr{tk, "CACHE does not accept allow_delete"})
				}
				res, err = NewCacheDirAccResolver(dir, limit, ttl, opts...)
			case "FULL":
				if ttl != 0 {
					*errors = append(*errors, &configErr{tk, "FULL does not accept ttl"})
				}
				res, err = NewDirAccResolver(dir, limit, sync, del, opts...)
			}
			if err != nil {
				*errors = append(*errors, &configErr{tk, err.Error()})
				return
			}
			o.AccountResolver = res
		default:
			err := &configErr{tk, fmt.Sprintf("error parsing operator resolver, wrong type %T", v)}
			*errors = append(*errors, err)
			return
		}
		if o.AccountResolver == nil {
			err := &configErr{tk, "error parsing account resolver, should be MEM or " +
				" URL(\"url\") or a map containing dir and type state=[FULL|CACHE])"}
			*errors = append(*errors, err)
		}
	case "resolver_tls":
		tc, err := parseTLS(tk, true)
		if err != nil {
			*errors = append(*errors, err)
			return
		}
		tlsConfig, err := GenTLSConfig(tc)
		if err != nil {
			err := &configErr{tk, err.Error()}
			*errors = append(*errors, err)
			return
		}
		o.AccountResolverTLSConfig = tlsConfig
		// GenTLSConfig loads the CA file into ClientCAs, but since this will
		// be used as a client connection, we need to set RootCAs.
		o.AccountResolverTLSConfig.RootCAs = tlsConfig.ClientCAs
	case "resolver_preload":
		mp, ok := v.(map[string]interface{})
		if !ok {
			err := &configErr{tk, "preload should be a map of account_public_key:account_jwt"}
			*errors = append(*errors, err)
			return
		}
		o.resolverPreloads = make(map[string]string)
		for key, val := range mp {
			tk, val = unwrapValue(val, &lt)
			if jwtstr, ok := val.(string); !ok {
				*errors = append(*errors, &configErr{tk, "preload map value should be a string JWT"})
				continue
			} else {
				// Make sure this is a valid account JWT, that is a config error.
				// We will warn of expirations, etc later.
				if _, err := jwt.DecodeAccountClaims(jwtstr); err != nil {
					err := &configErr{tk, "invalid account JWT"}
					*errors = append(*errors, err)
					continue
				}
				o.resolverPreloads[key] = jwtstr
			}
		}
	case "resolver_pinned_accounts":
		switch v := v.(type) {
		case string:
			o.resolverPinnedAccounts = map[string]struct{}{v: {}}
		case []string:
			o.resolverPinnedAccounts = make(map[string]struct{})
			for _, mv := range v {
				o.resolverPinnedAccounts[mv] = struct{}{}
			}
		case []interface{}:
			o.resolverPinnedAccounts = make(map[string]struct{})
			for _, mv := range v {
				tk, mv = unwrapValue(mv, &lt)
				if key, ok := mv.(string); ok {
					o.resolverPinnedAccounts[key] = struct{}{}
				} else {
					err := &configErr{tk,
						fmt.Sprintf("error parsing resolver_pinned_accounts: unsupported type in array %T", mv)}
					*errors = append(*errors, err)
					continue
				}
			}
		default:
			err := &configErr{tk, fmt.Sprintf("error parsing resolver_pinned_accounts: unsupported type %T", v)}
			*errors = append(*errors, err)
			return
		}
	case "no_auth_user":
		o.NoAuthUser = v.(string)
	case "system_account", "system":
		// Already processed at the beginning so we just skip them
		// to not treat them as unknown values.
		return
	case "no_system_account", "no_system", "no_sys_acc":
		o.NoSystemAccount = v.(bool)
	case "no_header_support":
		o.NoHeaderSupport = v.(bool)
	case "trusted", "trusted_keys":
		switch v := v.(type) {
		case string:
			o.TrustedKeys = []string{v}
		case []string:
			o.TrustedKeys = v
		case []interface{}:
			keys := make([]string, 0, len(v))
			for _, mv := range v {
				tk, mv = unwrapValue(mv, &lt)
				if key, ok := mv.(string); ok {
					keys = append(keys, key)
				} else {
					err := &configErr{tk, fmt.Sprintf("error parsing trusted: unsupported type in array %T", mv)}
					*errors = append(*errors, err)
					continue
				}
			}
			o.TrustedKeys = keys
		default:
			err := &configErr{tk, fmt.Sprintf("error parsing trusted: unsupported type %T", v)}
			*errors = append(*errors, err)
		}
		// Do a quick sanity check on keys
		for _, key := range o.TrustedKeys {
			if !nkeys.IsValidPublicOperatorKey(key) {
				err := &configErr{tk, fmt.Sprintf("trust key %q required to be a valid public operator nkey", key)}
				*errors = append(*errors, err)
			}
		}
	case "connect_error_reports":
		o.ConnectErrorReports = int(v.(int64))
	case "reconnect_error_reports":
		o.ReconnectErrorReports = int(v.(int64))
	case "websocket", "ws":
		if err := parseWebsocket(tk, o, errors, warnings); err != nil {
			*errors = append(*errors, err)
			return
		}
	case "mqtt":
		if err := parseMQTT(tk, o, errors, warnings); err != nil {
			*errors = append(*errors, err)
			return
		}
	case "server_tags":
		var err error
		switch v := v.(type) {
		case string:
			o.Tags.Add(v)
		case []string:
			o.Tags.Add(v...)
		case []interface{}:
			for _, t := range v {
				if token, ok := t.(token); ok {
					if ts, ok := token.Value().(string); ok {
						o.Tags.Add(ts)
						continue
					} else {
						err = &configErr{tk, fmt.Sprintf("error parsing tags: unsupported type %T where string is expected", token)}
					}
				} else {
					err = &configErr{tk, fmt.Sprintf("error parsing tags: unsupported type %T", t)}
				}
				break
			}
		default:
			err = &configErr{tk, fmt.Sprintf("error parsing tags: unsupported type %T", v)}
		}
		if err != nil {
			*errors = append(*errors, err)
			return
		}
	case "default_js_domain":
		vv, ok := v.(map[string]interface{})
		if !ok {
			*errors = append(*errors, &configErr{tk, fmt.Sprintf("error default_js_domain config: unsupported type %T", v)})
			return
		}
		m := make(map[string]string)
		for kk, kv := range vv {
			_, v = unwrapValue(kv, &tk)
			m[kk] = v.(string)
		}
		o.JsAccDefaultDomain = m
	default:
		if au := atomic.LoadInt32(&allowUnknownTopLevelField); au == 0 && !tk.IsUsedVariable() {
			err := &unknownConfigFieldErr{
				field: k,
				configErr: configErr{
					token: tk,
				},
			}
			*errors = append(*errors, err)
		}
	}
}

func setupUsersAndNKeysDuplicateCheckMap(o *Options) map[string]struct{} {
	unames := make(map[string]struct{}, len(o.Users)+len(o.Nkeys))
	for _, u := range o.Users {
		unames[u.Username] = struct{}{}
	}
	for _, u := range o.Nkeys {
		unames[u.Nkey] = struct{}{}
	}
	return unames
}

func parseDuration(field string, tk token, v interface{}, errors *[]error, warnings *[]error) time.Duration {
	if wd, ok := v.(string); ok {
		if dur, err := time.ParseDuration(wd); err != nil {
			err := &configErr{tk, fmt.Sprintf("error parsing %s: %v", field, err)}
			*errors = append(*errors, err)
			return 0
		} else {
			return dur
		}
	} else {
		// Backward compatible with old type, assume this is the
		// number of seconds.
		err := &configWarningErr{
			field: field,
			configErr: configErr{
				token:  tk,
				reason: field + " should be converted to a duration",
			},
		}
		*warnings = append(*warnings, err)
		return time.Duration(v.(int64)) * time.Second
	}
}

func trackExplicitVal(opts *Options, pm *map[string]bool, name string, val bool) {
	m := *pm
	if m == nil {
		m = make(map[string]bool)
		*pm = m
	}
	m[name] = val
}

// hostPort is simple struct to hold parsed listen/addr strings.
type hostPort struct {
	host string
	port int
}

// parseListen will parse listen option which is replacing host/net and port
func parseListen(v interface{}) (*hostPort, error) {
	hp := &hostPort{}
	switch vv := v.(type) {
	// Only a port
	case int64:
		hp.port = int(vv)
	case string:
		host, port, err := net.SplitHostPort(vv)
		if err != nil {
			return nil, fmt.Errorf("could not parse address string %q", vv)
		}
		hp.port, err = strconv.Atoi(port)
		if err != nil {
			return nil, fmt.Errorf("could not parse port %q", port)
		}
		hp.host = host
	default:
		return nil, fmt.Errorf("expected port or host:port, got %T", vv)
	}
	return hp, nil
}

// parseCluster will parse the cluster config.
func parseCluster(v interface{}, opts *Options, errors *[]error, warnings *[]error) error {
	var lt token
	defer convertPanicToErrorList(&lt, errors)

	tk, v := unwrapValue(v, &lt)
	cm, ok := v.(map[string]interface{})
	if !ok {
		return &configErr{tk, fmt.Sprintf("Expected map to define cluster, got %T", v)}
	}

	for mk, mv := range cm {
		// Again, unwrap token value if line check is required.
		tk, mv = unwrapValue(mv, &lt)
		switch strings.ToLower(mk) {
		case "name":
			opts.Cluster.Name = mv.(string)
		case "listen":
			hp, err := parseListen(mv)
			if err != nil {
				err := &configErr{tk, err.Error()}
				*errors = append(*errors, err)
				continue
			}
			opts.Cluster.Host = hp.host
			opts.Cluster.Port = hp.port
		case "port":
			opts.Cluster.Port = int(mv.(int64))
		case "host", "net":
			opts.Cluster.Host = mv.(string)
		case "authorization":
			auth, err := parseAuthorization(tk, opts, errors, warnings)
			if err != nil {
				*errors = append(*errors, err)
				continue
			}
			if auth.users != nil {
				err := &configErr{tk, "Cluster authorization does not allow multiple users"}
				*errors = append(*errors, err)
				continue
			}
			if auth.token != _EMPTY_ {
				err := &configErr{tk, "Cluster authorization does not support tokens"}
				*errors = append(*errors, err)
				continue
			}
			opts.Cluster.Username = auth.user
			opts.Cluster.Password = auth.pass
			opts.Cluster.AuthTimeout = auth.timeout

			if auth.defaultPermissions != nil {
				err := &configWarningErr{
					field: mk,
					configErr: configErr{
						token:  tk,
						reason: `setting "permissions" within cluster authorization block is deprecated`,
					},
				}
				*warnings = append(*warnings, err)

				// Do not set permissions if they were specified in top-level cluster block.
				if opts.Cluster.Permissions == nil {
					setClusterPermissions(&opts.Cluster, auth.defaultPermissions)
				}
			}
		case "routes":
			ra := mv.([]interface{})
			routes, errs := parseURLs(ra, "route", warnings)
			if errs != nil {
				*errors = append(*errors, errs...)
				continue
			}
			opts.Routes = routes
		case "tls":
			config, tlsopts, err := getTLSConfig(tk)
			if err != nil {
				*errors = append(*errors, err)
				continue
			}
			opts.Cluster.TLSConfig = config
			opts.Cluster.TLSTimeout = tlsopts.Timeout
			opts.Cluster.TLSMap = tlsopts.Map
			opts.Cluster.TLSPinnedCerts = tlsopts.PinnedCerts
			opts.Cluster.TLSCheckKnownURLs = tlsopts.TLSCheckKnownURLs
			opts.Cluster.tlsConfigOpts = tlsopts
		case "cluster_advertise", "advertise":
			opts.Cluster.Advertise = mv.(string)
		case "no_advertise":
			opts.Cluster.NoAdvertise = mv.(bool)
			trackExplicitVal(opts, &opts.inConfig, "Cluster.NoAdvertise", opts.Cluster.NoAdvertise)
		case "connect_retries":
			opts.Cluster.ConnectRetries = int(mv.(int64))
		case "permissions":
			perms, err := parseUserPermissions(mv, errors, warnings)
			if err != nil {
				*errors = append(*errors, err)
				continue
			}
			// Dynamic response permissions do not make sense here.
			if perms.Response != nil {
				err := &configErr{tk, "Cluster permissions do not support dynamic responses"}
				*errors = append(*errors, err)
				continue
			}
			// This will possibly override permissions that were define in auth block
			setClusterPermissions(&opts.Cluster, perms)
		default:
			if !tk.IsUsedVariable() {
				err := &unknownConfigFieldErr{
					field: mk,
					configErr: configErr{
						token: tk,
					},
				}
				*errors = append(*errors, err)
				continue
			}
		}
	}
	return nil
}

func parseURLs(a []interface{}, typ string, warnings *[]error) (urls []*url.URL, errors []error) {
	urls = make([]*url.URL, 0, len(a))
	var lt token
	defer convertPanicToErrorList(&lt, &errors)

	dd := make(map[string]bool)

	for _, u := range a {
		tk, u := unwrapValue(u, &lt)
		sURL := u.(string)
		if dd[sURL] {
			err := &configWarningErr{
				field: sURL,
				configErr: configErr{
					token:  tk,
					reason: fmt.Sprintf("Duplicate %s entry detected", typ),
				},
			}
			*warnings = append(*warnings, err)
			continue
		}
		dd[sURL] = true
		url, err := parseURL(sURL, typ)
		if err != nil {
			err := &configErr{tk, err.Error()}
			errors = append(errors, err)
			continue
		}
		urls = append(urls, url)
	}
	return urls, errors
}

func parseURL(u string, typ string) (*url.URL, error) {
	urlStr := strings.TrimSpace(u)
	url, err := url.Parse(urlStr)
	if err != nil {
		// Security note: if it's not well-formed but still reached us, then we're going to log as-is which might include password information here.
		// If the URL parses, we don't log the credentials ever, but if it doesn't even parse we don't have a sane way to redact.
		return nil, fmt.Errorf("error parsing %s url [%q]", typ, urlStr)
	}
	return url, nil
}

func parseGateway(v interface{}, o *Options, errors *[]error, warnings *[]error) error {
	var lt token
	defer convertPanicToErrorList(&lt, errors)

	tk, v := unwrapValue(v, &lt)
	gm, ok := v.(map[string]interface{})
	if !ok {
		return &configErr{tk, fmt.Sprintf("Expected gateway to be a map, got %T", v)}
	}
	for mk, mv := range gm {
		// Again, unwrap token value if line check is required.
		tk, mv = unwrapValue(mv, &lt)
		switch strings.ToLower(mk) {
		case "name":
			o.Gateway.Name = mv.(string)
		case "listen":
			hp, err := parseListen(mv)
			if err != nil {
				err := &configErr{tk, err.Error()}
				*errors = append(*errors, err)
				continue
			}
			o.Gateway.Host = hp.host
			o.Gateway.Port = hp.port
		case "port":
			o.Gateway.Port = int(mv.(int64))
		case "host", "net":
			o.Gateway.Host = mv.(string)
		case "authorization":
			auth, err := parseAuthorization(tk, o, errors, warnings)
			if err != nil {
				*errors = append(*errors, err)
				continue
			}
			if auth.users != nil {
				*errors = append(*errors, &configErr{tk, "Gateway authorization does not allow multiple users"})
				continue
			}
			if auth.token != _EMPTY_ {
				err := &configErr{tk, "Gateway authorization does not support tokens"}
				*errors = append(*errors, err)
				continue
			}
			o.Gateway.Username = auth.user
			o.Gateway.Password = auth.pass
			o.Gateway.AuthTimeout = auth.timeout
		case "tls":
			config, tlsopts, err := getTLSConfig(tk)
			if err != nil {
				*errors = append(*errors, err)
				continue
			}
			o.Gateway.TLSConfig = config
			o.Gateway.TLSTimeout = tlsopts.Timeout
			o.Gateway.TLSMap = tlsopts.Map
			o.Gateway.TLSCheckKnownURLs = tlsopts.TLSCheckKnownURLs
			o.Gateway.TLSPinnedCerts = tlsopts.PinnedCerts
			o.Gateway.tlsConfigOpts = tlsopts
		case "advertise":
			o.Gateway.Advertise = mv.(string)
		case "connect_retries":
			o.Gateway.ConnectRetries = int(mv.(int64))
		case "gateways":
			gateways, err := parseGateways(mv, errors, warnings)
			if err != nil {
				return err
			}
			o.Gateway.Gateways = gateways
		case "reject_unknown", "reject_unknown_cluster":
			o.Gateway.RejectUnknown = mv.(bool)
		default:
			if !tk.IsUsedVariable() {
				err := &unknownConfigFieldErr{
					field: mk,
					configErr: configErr{
						token: tk,
					},
				}
				*errors = append(*errors, err)
				continue
			}
		}
	}
	return nil
}

var dynamicJSAccountLimits = JetStreamAccountLimits{-1, -1, -1, -1, -1, -1, -1, false}
var defaultJSAccountTiers = map[string]JetStreamAccountLimits{_EMPTY_: dynamicJSAccountLimits}

// Parses jetstream account limits for an account. Simple setup with boolen is allowed, and we will
// use dynamic account limits.
func parseJetStreamForAccount(v interface{}, acc *Account, errors *[]error, warnings *[]error) error {
	var lt token

	tk, v := unwrapValue(v, &lt)

	// Value here can be bool, or string "enabled" or a map.
	switch vv := v.(type) {
	case bool:
		if vv {
			acc.jsLimits = defaultJSAccountTiers
		}
	case string:
		switch strings.ToLower(vv) {
		case "enabled", "enable":
			acc.jsLimits = defaultJSAccountTiers
		case "disabled", "disable":
			acc.jsLimits = nil
		default:
			return &configErr{tk, fmt.Sprintf("Expected 'enabled' or 'disabled' for string value, got '%s'", vv)}
		}
	case map[string]interface{}:
		jsLimits := JetStreamAccountLimits{-1, -1, -1, -1, -1, -1, -1, false}
		for mk, mv := range vv {
			tk, mv = unwrapValue(mv, &lt)
			switch strings.ToLower(mk) {
			case "max_memory", "max_mem", "mem", "memory":
				vv, ok := mv.(int64)
				if !ok {
					return &configErr{tk, fmt.Sprintf("Expected a parseable size for %q, got %v", mk, mv)}
				}
				jsLimits.MaxMemory = vv
			case "max_store", "max_file", "max_disk", "store", "disk":
				vv, ok := mv.(int64)
				if !ok {
					return &configErr{tk, fmt.Sprintf("Expected a parseable size for %q, got %v", mk, mv)}
				}
				jsLimits.MaxStore = vv
			case "max_streams", "streams":
				vv, ok := mv.(int64)
				if !ok {
					return &configErr{tk, fmt.Sprintf("Expected a parseable size for %q, got %v", mk, mv)}
				}
				jsLimits.MaxStreams = int(vv)
			case "max_consumers", "consumers":
				vv, ok := mv.(int64)
				if !ok {
					return &configErr{tk, fmt.Sprintf("Expected a parseable size for %q, got %v", mk, mv)}
				}
				jsLimits.MaxConsumers = int(vv)
			case "max_bytes_required", "max_stream_bytes", "max_bytes":
				vv, ok := mv.(bool)
				if !ok {
					return &configErr{tk, fmt.Sprintf("Expected a parseable bool for %q, got %v", mk, mv)}
				}
				jsLimits.MaxBytesRequired = vv
			case "mem_max_stream_bytes", "memory_max_stream_bytes":
				vv, ok := mv.(int64)
				if !ok {
					return &configErr{tk, fmt.Sprintf("Expected a parseable size for %q, got %v", mk, mv)}
				}
				jsLimits.MemoryMaxStreamBytes = vv
			case "disk_max_stream_bytes", "store_max_stream_bytes":
				vv, ok := mv.(int64)
				if !ok {
					return &configErr{tk, fmt.Sprintf("Expected a parseable size for %q, got %v", mk, mv)}
				}
				jsLimits.StoreMaxStreamBytes = vv
			case "max_ack_pending":
				vv, ok := mv.(int64)
				if !ok {
					return &configErr{tk, fmt.Sprintf("Expected a parseable size for %q, got %v", mk, mv)}
				}
				jsLimits.MaxAckPending = int(vv)
			default:
				if !tk.IsUsedVariable() {
					err := &unknownConfigFieldErr{
						field: mk,
						configErr: configErr{
							token: tk,
						},
					}
					*errors = append(*errors, err)
					continue
				}
			}
		}
		acc.jsLimits = map[string]JetStreamAccountLimits{_EMPTY_: jsLimits}
	default:
		return &configErr{tk, fmt.Sprintf("Expected map, bool or string to define JetStream, got %T", v)}
	}
	return nil
}

// takes in a storage size as either an int or a string and returns an int64 value based on the input.
func getStorageSize(v interface{}) (int64, error) {
	_, ok := v.(int64)
	if ok {
		return v.(int64), nil
	}

	s, ok := v.(string)
	if !ok {
		return 0, fmt.Errorf("must be int64 or string")
	}

	if s == "" {
		return 0, nil
	}

	suffix := s[len(s)-1:]
	prefix := s[:len(s)-1]
	num, err := strconv.ParseInt(prefix, 10, 64)
	if err != nil {
		return 0, err
	}

	suffixMap := map[string]int64{"K": 10, "M": 20, "G": 30, "T": 40}

	mult, ok := suffixMap[suffix]
	if !ok {
		return 0, fmt.Errorf("sizes defined as strings must end in K, M, G, T")
	}
	num *= 1 << mult

	return num, nil
}

// Parse enablement of jetstream for a server.
func parseJetStreamLimits(v interface{}, opts *Options, errors *[]error, warnings *[]error) error {
	var lt token
	tk, v := unwrapValue(v, &lt)

	lim := JSLimitOpts{}

	vv, ok := v.(map[string]interface{})
	if !ok {
		return &configErr{tk, fmt.Sprintf("Expected a map to define JetStreamLimits, got %T", v)}
	}
	for mk, mv := range vv {
		tk, mv = unwrapValue(mv, &lt)
		switch strings.ToLower(mk) {
		case "max_ack_pending":
			lim.MaxAckPending = int(mv.(int64))
		case "max_ha_assets":
			lim.MaxHAAssets = int(mv.(int64))
		case "max_request_batch":
			lim.MaxRequestBatch = int(mv.(int64))
		case "duplicate_window":
			var err error
			lim.Duplicates, err = time.ParseDuration(mv.(string))
			if err != nil {
				*errors = append(*errors, err)
			}
		default:
			if !tk.IsUsedVariable() {
				err := &unknownConfigFieldErr{
					field: mk,
					configErr: configErr{
						token: tk,
					},
				}
				*errors = append(*errors, err)
				continue
			}
		}
	}
	opts.JetStreamLimits = lim
	return nil
}

// Parse enablement of jetstream for a server.
func parseJetStream(v interface{}, opts *Options, errors *[]error, warnings *[]error) error {
	var lt token

	tk, v := unwrapValue(v, &lt)

	// Value here can be bool, or string "enabled" or a map.
	switch vv := v.(type) {
	case bool:
		opts.JetStream = v.(bool)
	case string:
		switch strings.ToLower(vv) {
		case "enabled", "enable":
			opts.JetStream = true
		case "disabled", "disable":
			opts.JetStream = false
		default:
			return &configErr{tk, fmt.Sprintf("Expected 'enabled' or 'disabled' for string value, got '%s'", vv)}
		}
	case map[string]interface{}:
		doEnable := true
		for mk, mv := range vv {
			tk, mv = unwrapValue(mv, &lt)
			switch strings.ToLower(mk) {
			case "store", "store_dir", "storedir":
				// StoreDir can be set at the top level as well so have to prevent ambiguous declarations.
				if opts.StoreDir != _EMPTY_ {
					return &configErr{tk, "Duplicate 'store_dir' configuration"}
				}
				opts.StoreDir = mv.(string)
			case "max_memory_store", "max_mem_store", "max_mem":
				s, err := getStorageSize(mv)
				if err != nil {
					return &configErr{tk, fmt.Sprintf("max_mem_store %s", err)}
				}
				opts.JetStreamMaxMemory = s
				opts.maxMemSet = true
			case "max_file_store", "max_file":
				s, err := getStorageSize(mv)
				if err != nil {
					return &configErr{tk, fmt.Sprintf("max_file_store %s", err)}
				}
				opts.JetStreamMaxStore = s
				opts.maxStoreSet = true
			case "domain":
				opts.JetStreamDomain = mv.(string)
			case "enable", "enabled":
				doEnable = mv.(bool)
			case "key", "ek", "encryption_key":
				opts.JetStreamKey = mv.(string)
			case "cipher":
				switch strings.ToLower(mv.(string)) {
				case "chacha", "chachapoly":
					opts.JetStreamCipher = ChaCha
				case "aes":
					opts.JetStreamCipher = AES
				default:
					return &configErr{tk, fmt.Sprintf("Unknown cipher type: %q", mv)}
				}
			case "extension_hint":
				opts.JetStreamExtHint = mv.(string)
			case "limits":
				if err := parseJetStreamLimits(tk, opts, errors, warnings); err != nil {
					return err
				}
			case "unique_tag":
				opts.JetStreamUniqueTag = strings.ToLower(strings.TrimSpace(mv.(string)))
			case "max_outstanding_catchup":
				s, err := getStorageSize(mv)
				if err != nil {
					return &configErr{tk, fmt.Sprintf("%s %s", strings.ToLower(mk), err)}
				}
				opts.JetStreamMaxCatchup = s
			default:
				if !tk.IsUsedVariable() {
					err := &unknownConfigFieldErr{
						field: mk,
						configErr: configErr{
							token: tk,
						},
					}
					*errors = append(*errors, err)
					continue
				}
			}
		}
		opts.JetStream = doEnable
	default:
		return &configErr{tk, fmt.Sprintf("Expected map, bool or string to define JetStream, got %T", v)}
	}

	return nil
}

// parseLeafNodes will parse the leaf node config.
func parseLeafNodes(v interface{}, opts *Options, errors *[]error, warnings *[]error) error {
	var lt token
	defer convertPanicToErrorList(&lt, errors)

	tk, v := unwrapValue(v, &lt)
	cm, ok := v.(map[string]interface{})
	if !ok {
		return &configErr{tk, fmt.Sprintf("Expected map to define a leafnode, got %T", v)}
	}

	for mk, mv := range cm {
		// Again, unwrap token value if line check is required.
		tk, mv = unwrapValue(mv, &lt)
		switch strings.ToLower(mk) {
		case "listen":
			hp, err := parseListen(mv)
			if err != nil {
				err := &configErr{tk, err.Error()}
				*errors = append(*errors, err)
				continue
			}
			opts.LeafNode.Host = hp.host
			opts.LeafNode.Port = hp.port
		case "port":
			opts.LeafNode.Port = int(mv.(int64))
		case "host", "net":
			opts.LeafNode.Host = mv.(string)
		case "authorization":
			auth, err := parseLeafAuthorization(tk, errors, warnings)
			if err != nil {
				*errors = append(*errors, err)
				continue
			}
			opts.LeafNode.Username = auth.user
			opts.LeafNode.Password = auth.pass
			opts.LeafNode.AuthTimeout = auth.timeout
			opts.LeafNode.Account = auth.acc
			opts.LeafNode.Users = auth.users
			// Validate user info config for leafnode authorization
			if err := validateLeafNodeAuthOptions(opts); err != nil {
				*errors = append(*errors, &configErr{tk, err.Error()})
				continue
			}
		case "remotes":
			// Parse the remote options here.
			remotes, err := parseRemoteLeafNodes(tk, errors, warnings)
			if err != nil {
				*errors = append(*errors, err)
				continue
			}
			opts.LeafNode.Remotes = remotes
		case "reconnect", "reconnect_delay", "reconnect_interval":
			opts.LeafNode.ReconnectInterval = time.Duration(int(mv.(int64))) * time.Second
		case "tls":
			tc, err := parseTLS(tk, true)
			if err != nil {
				*errors = append(*errors, err)
				continue
			}
			if opts.LeafNode.TLSConfig, err = GenTLSConfig(tc); err != nil {
				err := &configErr{tk, err.Error()}
				*errors = append(*errors, err)
				continue
			}
			opts.LeafNode.TLSTimeout = tc.Timeout
			opts.LeafNode.TLSMap = tc.Map
			opts.LeafNode.TLSPinnedCerts = tc.PinnedCerts
			opts.LeafNode.tlsConfigOpts = tc
		case "leafnode_advertise", "advertise":
			opts.LeafNode.Advertise = mv.(string)
		case "no_advertise":
			opts.LeafNode.NoAdvertise = mv.(bool)
			trackExplicitVal(opts, &opts.inConfig, "LeafNode.NoAdvertise", opts.LeafNode.NoAdvertise)
		case "min_version", "minimum_version":
			version := mv.(string)
			if err := checkLeafMinVersionConfig(version); err != nil {
				err = &configErr{tk, err.Error()}
				*errors = append(*errors, err)
				continue
			}
			opts.LeafNode.MinVersion = version
		default:
			if !tk.IsUsedVariable() {
				err := &unknownConfigFieldErr{
					field: mk,
					configErr: configErr{
						token: tk,
					},
				}
				*errors = append(*errors, err)
				continue
			}
		}
	}
	return nil
}

// This is the authorization parser adapter for the leafnode's
// authorization config.
func parseLeafAuthorization(v interface{}, errors *[]error, warnings *[]error) (*authorization, error) {
	var (
		am   map[string]interface{}
		tk   token
		lt   token
		auth = &authorization{}
	)
	defer convertPanicToErrorList(&lt, errors)

	_, v = unwrapValue(v, &lt)
	am = v.(map[string]interface{})
	for mk, mv := range am {
		tk, mv = unwrapValue(mv, &lt)
		switch strings.ToLower(mk) {
		case "user", "username":
			auth.user = mv.(string)
		case "pass", "password":
			auth.pass = mv.(string)
		case "timeout":
			at := float64(1)
			switch mv := mv.(type) {
			case int64:
				at = float64(mv)
			case float64:
				at = mv
			}
			auth.timeout = at
		case "users":
			users, err := parseLeafUsers(tk, errors, warnings)
			if err != nil {
				*errors = append(*errors, err)
				continue
			}
			auth.users = users
		case "account":
			auth.acc = mv.(string)
		default:
			if !tk.IsUsedVariable() {
				err := &unknownConfigFieldErr{
					field: mk,
					configErr: configErr{
						token: tk,
					},
				}
				*errors = append(*errors, err)
			}
			continue
		}
	}
	return auth, nil
}

// This is a trimmed down version of parseUsers that is adapted
// for the users possibly defined in the authorization{} section
// of leafnodes {}.
func parseLeafUsers(mv interface{}, errors *[]error, warnings *[]error) ([]*User, error) {
	var (
		tk    token
		lt    token
		users = []*User{}
	)
	defer convertPanicToErrorList(&lt, errors)

	tk, mv = unwrapValue(mv, &lt)
	// Make sure we have an array
	uv, ok := mv.([]interface{})
	if !ok {
		return nil, &configErr{tk, fmt.Sprintf("Expected users field to be an array, got %v", mv)}
	}
	for _, u := range uv {
		tk, u = unwrapValue(u, &lt)
		// Check its a map/struct
		um, ok := u.(map[string]interface{})
		if !ok {
			err := &configErr{tk, fmt.Sprintf("Expected user entry to be a map/struct, got %v", u)}
			*errors = append(*errors, err)
			continue
		}
		user := &User{}
		for k, v := range um {
			tk, v = unwrapValue(v, &lt)
			switch strings.ToLower(k) {
			case "user", "username":
				user.Username = v.(string)
			case "pass", "password":
				user.Password = v.(string)
			case "account":
				// We really want to save just the account name here, but
				// the User object is *Account. So we create an account object
				// but it won't be registered anywhere. The server will just
				// use opts.LeafNode.Users[].Account.Name. Alternatively
				// we need to create internal objects to store u/p and account
				// name and have a server structure to hold that.
				user.Account = NewAccount(v.(string))
			default:
				if !tk.IsUsedVariable() {
					err := &unknownConfigFieldErr{
						field: k,
						configErr: configErr{
							token: tk,
						},
					}
					*errors = append(*errors, err)
					continue
				}
			}
		}
		users = append(users, user)
	}
	return users, nil
}

func parseRemoteLeafNodes(v interface{}, errors *[]error, warnings *[]error) ([]*RemoteLeafOpts, error) {
	var lt token
	defer convertPanicToErrorList(&lt, errors)
	tk, v := unwrapValue(v, &lt)
	ra, ok := v.([]interface{})
	if !ok {
		return nil, &configErr{tk, fmt.Sprintf("Expected remotes field to be an array, got %T", v)}
	}
	remotes := make([]*RemoteLeafOpts, 0, len(ra))
	for _, r := range ra {
		tk, r = unwrapValue(r, &lt)
		// Check its a map/struct
		rm, ok := r.(map[string]interface{})
		if !ok {
			*errors = append(*errors, &configErr{tk, fmt.Sprintf("Expected remote leafnode entry to be a map/struct, got %v", r)})
			continue
		}
		remote := &RemoteLeafOpts{}
		for k, v := range rm {
			tk, v = unwrapValue(v, &lt)
			switch strings.ToLower(k) {
			case "no_randomize", "dont_randomize":
				remote.NoRandomize = v.(bool)
			case "url", "urls":
				switch v := v.(type) {
				case []interface{}, []string:
					urls, errs := parseURLs(v.([]interface{}), "leafnode", warnings)
					if errs != nil {
						*errors = append(*errors, errs...)
						continue
					}
					remote.URLs = urls
				case string:
					url, err := parseURL(v, "leafnode")
					if err != nil {
						*errors = append(*errors, &configErr{tk, err.Error()})
						continue
					}
					remote.URLs = append(remote.URLs, url)
				default:
					*errors = append(*errors, &configErr{tk, fmt.Sprintf("Expected remote leafnode url to be an array or string, got %v", v)})
					continue
				}
			case "account", "local":
				remote.LocalAccount = v.(string)
			case "creds", "credentials":
				p, err := expandPath(v.(string))
				if err != nil {
					*errors = append(*errors, &configErr{tk, err.Error()})
					continue
				}
				remote.Credentials = p
			case "tls":
				tc, err := parseTLS(tk, true)
				if err != nil {
					*errors = append(*errors, err)
					continue
				}
				if remote.TLSConfig, err = GenTLSConfig(tc); err != nil {
					*errors = append(*errors, &configErr{tk, err.Error()})
					continue
				}
				// If ca_file is defined, GenTLSConfig() sets TLSConfig.ClientCAs.
				// Set RootCAs since this tls.Config is used when soliciting
				// a connection (therefore behaves as a client).
				remote.TLSConfig.RootCAs = remote.TLSConfig.ClientCAs
				if tc.Timeout > 0 {
					remote.TLSTimeout = tc.Timeout
				} else {
					remote.TLSTimeout = float64(DEFAULT_LEAF_TLS_TIMEOUT) / float64(time.Second)
				}
				remote.tlsConfigOpts = tc
			case "hub":
				remote.Hub = v.(bool)
			case "deny_imports", "deny_import":
				subjects, err := parsePermSubjects(tk, errors, warnings)
				if err != nil {
					*errors = append(*errors, err)
					continue
				}
				remote.DenyImports = subjects
			case "deny_exports", "deny_export":
				subjects, err := parsePermSubjects(tk, errors, warnings)
				if err != nil {
					*errors = append(*errors, err)
					continue
				}
				remote.DenyExports = subjects
			case "ws_compress", "ws_compression", "websocket_compress", "websocket_compression":
				remote.Websocket.Compression = v.(bool)
			case "ws_no_masking", "websocket_no_masking":
				remote.Websocket.NoMasking = v.(bool)
			case "jetstream_cluster_migrate", "js_cluster_migrate":
				remote.JetStreamClusterMigrate = true
			default:
				if !tk.IsUsedVariable() {
					err := &unknownConfigFieldErr{
						field: k,
						configErr: configErr{
							token: tk,
						},
					}
					*errors = append(*errors, err)
					continue
				}
			}
		}
		remotes = append(remotes, remote)
	}
	return remotes, nil
}

// Parse TLS and returns a TLSConfig and TLSTimeout.
// Used by cluster and gateway parsing.
func getTLSConfig(tk token) (*tls.Config, *TLSConfigOpts, error) {
	tc, err := parseTLS(tk, false)
	if err != nil {
		return nil, nil, err
	}
	config, err := GenTLSConfig(tc)
	if err != nil {
		err := &configErr{tk, err.Error()}
		return nil, nil, err
	}
	// For clusters/gateways, we will force strict verification. We also act
	// as both client and server, so will mirror the rootCA to the
	// clientCA pool.
	config.ClientAuth = tls.RequireAndVerifyClientCert
	config.RootCAs = config.ClientCAs
	return config, tc, nil
}

func parseGateways(v interface{}, errors *[]error, warnings *[]error) ([]*RemoteGatewayOpts, error) {
	var lt token
	defer convertPanicToErrorList(&lt, errors)

	tk, v := unwrapValue(v, &lt)
	// Make sure we have an array
	ga, ok := v.([]interface{})
	if !ok {
		return nil, &configErr{tk, fmt.Sprintf("Expected gateways field to be an array, got %T", v)}
	}
	gateways := []*RemoteGatewayOpts{}
	for _, g := range ga {
		tk, g = unwrapValue(g, &lt)
		// Check its a map/struct
		gm, ok := g.(map[string]interface{})
		if !ok {
			*errors = append(*errors, &configErr{tk, fmt.Sprintf("Expected gateway entry to be a map/struct, got %v", g)})
			continue
		}
		gateway := &RemoteGatewayOpts{}
		for k, v := range gm {
			tk, v = unwrapValue(v, &lt)
			switch strings.ToLower(k) {
			case "name":
				gateway.Name = v.(string)
			case "tls":
				tls, tlsopts, err := getTLSConfig(tk)
				if err != nil {
					*errors = append(*errors, err)
					continue
				}
				gateway.TLSConfig = tls
				gateway.TLSTimeout = tlsopts.Timeout
				gateway.tlsConfigOpts = tlsopts
			case "url":
				url, err := parseURL(v.(string), "gateway")
				if err != nil {
					*errors = append(*errors, &configErr{tk, err.Error()})
					continue
				}
				gateway.URLs = append(gateway.URLs, url)
			case "urls":
				urls, errs := parseURLs(v.([]interface{}), "gateway", warnings)
				if errs != nil {
					*errors = append(*errors, errs...)
					continue
				}
				gateway.URLs = urls
			default:
				if !tk.IsUsedVariable() {
					err := &unknownConfigFieldErr{
						field: k,
						configErr: configErr{
							token: tk,
						},
					}
					*errors = append(*errors, err)
					continue
				}
			}
		}
		gateways = append(gateways, gateway)
	}
	return gateways, nil
}

// Sets cluster's permissions based on given pub/sub permissions,
// doing the appropriate translation.
func setClusterPermissions(opts *ClusterOpts, perms *Permissions) {
	// Import is whether or not we will send a SUB for interest to the other side.
	// Export is whether or not we will accept a SUB from the remote for a given subject.
	// Both only effect interest registration.
	// The parsing sets Import into Publish and Export into Subscribe, convert
	// accordingly.
	opts.Permissions = &RoutePermissions{
		Import: perms.Publish,
		Export: perms.Subscribe,
	}
}

// Temp structures to hold account import and export defintions since they need
// to be processed after being parsed.
type export struct {
	acc  *Account
	sub  string
	accs []string
	rt   ServiceRespType
	lat  *serviceLatency
	rthr time.Duration
	tPos uint
}

type importStream struct {
	acc *Account
	an  string
	sub string
	to  string
	pre string
}

type importService struct {
	acc   *Account
	an    string
	sub   string
	to    string
	share bool
}

// Checks if an account name is reserved.
func isReservedAccount(name string) bool {
	return name == globalAccountName
}

func parseAccountMapDest(v interface{}, tk token, errors *[]error, warnings *[]error) (*MapDest, *configErr) {
	// These should be maps.
	mv, ok := v.(map[string]interface{})
	if !ok {
		err := &configErr{tk, "Expected an entry for the mapping destination"}
		*errors = append(*errors, err)
		return nil, err
	}

	mdest := &MapDest{}
	var lt token
	var sw bool

	for k, v := range mv {
		tk, dmv := unwrapValue(v, &lt)
		switch strings.ToLower(k) {
		case "dest", "destination":
			mdest.Subject = dmv.(string)
		case "weight":
			switch vv := dmv.(type) {
			case string:
				ws := vv
				ws = strings.TrimSuffix(ws, "%")
				weight, err := strconv.Atoi(ws)
				if err != nil {
					err := &configErr{tk, fmt.Sprintf("Invalid weight %q for mapping destination", ws)}
					*errors = append(*errors, err)
					return nil, err
				}
				if weight > 100 || weight < 0 {
					err := &configErr{tk, fmt.Sprintf("Invalid weight %d for mapping destination", weight)}
					*errors = append(*errors, err)
					return nil, err
				}
				mdest.Weight = uint8(weight)
				sw = true
			case int64:
				weight := vv
				if weight > 100 || weight < 0 {
					err := &configErr{tk, fmt.Sprintf("Invalid weight %d for mapping destination", weight)}
					*errors = append(*errors, err)
					return nil, err
				}
				mdest.Weight = uint8(weight)
				sw = true
			default:
				err := &configErr{tk, fmt.Sprintf("Unknown entry type for weight of %v\n", vv)}
				*errors = append(*errors, err)
				return nil, err
			}
		case "cluster":
			mdest.Cluster = dmv.(string)
		default:
			err := &configErr{tk, fmt.Sprintf("Unknown field %q for mapping destination", k)}
			*errors = append(*errors, err)
			return nil, err
		}
	}

	if !sw {
		err := &configErr{tk, fmt.Sprintf("Missing weight for mapping destination %q", mdest.Subject)}
		*errors = append(*errors, err)
		return nil, err
	}

	return mdest, nil
}

// parseAccountMappings is called to parse account mappings.
func parseAccountMappings(v interface{}, acc *Account, errors *[]error, warnings *[]error) error {
	var lt token
	defer convertPanicToErrorList(&lt, errors)

	tk, v := unwrapValue(v, &lt)
	am := v.(map[string]interface{})
	for subj, mv := range am {
		if !IsValidSubject(subj) {
			err := &configErr{tk, fmt.Sprintf("Subject %q is not a valid subject", subj)}
			*errors = append(*errors, err)
			continue
		}
		tk, v := unwrapValue(mv, &lt)

		switch vv := v.(type) {
		case string:
			if err := acc.AddMapping(subj, v.(string)); err != nil {
				err := &configErr{tk, fmt.Sprintf("Error adding mapping for %q to %q : %v", subj, v.(string), err)}
				*errors = append(*errors, err)
				continue
			}
		case []interface{}:
			var mappings []*MapDest
			for _, mv := range v.([]interface{}) {
				tk, amv := unwrapValue(mv, &lt)
				mdest, err := parseAccountMapDest(amv, tk, errors, warnings)
				if err != nil {
					continue
				}
				mappings = append(mappings, mdest)
			}

			// Now add them in..
			if err := acc.AddWeightedMappings(subj, mappings...); err != nil {
				err := &configErr{tk, fmt.Sprintf("Error adding mapping for %q to %q : %v", subj, v.(string), err)}
				*errors = append(*errors, err)
				continue
			}
		case interface{}:
			tk, amv := unwrapValue(mv, &lt)
			mdest, err := parseAccountMapDest(amv, tk, errors, warnings)
			if err != nil {
				continue
			}
			// Now add it in..
			if err := acc.AddWeightedMappings(subj, mdest); err != nil {
				err := &configErr{tk, fmt.Sprintf("Error adding mapping for %q to %q : %v", subj, v.(string), err)}
				*errors = append(*errors, err)
				continue
			}
		default:
			err := &configErr{tk, fmt.Sprintf("Unknown type %T for mapping destination", vv)}
			*errors = append(*errors, err)
			continue
		}
	}

	return nil
}

// parseAccountLimits is called to parse account limits in a server config.
func parseAccountLimits(mv interface{}, acc *Account, errors *[]error, warnings *[]error) error {
	var lt token
	defer convertPanicToErrorList(&lt, errors)

	tk, v := unwrapValue(mv, &lt)
	am, ok := v.(map[string]interface{})
	if !ok {
		return &configErr{tk, fmt.Sprintf("Expected account limits to be a map/struct, got %+v", v)}
	}

	for k, v := range am {
		tk, mv = unwrapValue(v, &lt)
		switch strings.ToLower(k) {
		case "max_connections", "max_conn":
			acc.mconns = int32(mv.(int64))
		case "max_subscriptions", "max_subs":
			acc.msubs = int32(mv.(int64))
		case "max_payload", "max_pay":
			acc.mpay = int32(mv.(int64))
		case "max_leafnodes", "max_leafs":
			acc.mleafs = int32(mv.(int64))
		default:
			if !tk.IsUsedVariable() {
				err := &configErr{tk, fmt.Sprintf("Unknown field %q parsing account limits", k)}
				*errors = append(*errors, err)
			}
		}
	}

	return nil
}

// parseAccounts will parse the different accounts syntax.
func parseAccounts(v interface{}, opts *Options, errors *[]error, warnings *[]error) error {
	var (
		importStreams  []*importStream
		importServices []*importService
		exportStreams  []*export
		exportServices []*export
		lt             token
	)
	defer convertPanicToErrorList(&lt, errors)

	tk, v := unwrapValue(v, &lt)
	switch vv := v.(type) {
	// Simple array of account names.
	case []interface{}, []string:
		m := make(map[string]struct{}, len(v.([]interface{})))
		for _, n := range v.([]interface{}) {
			tk, name := unwrapValue(n, &lt)
			ns := name.(string)
			// Check for reserved names.
			if isReservedAccount(ns) {
				err := &configErr{tk, fmt.Sprintf("%q is a Reserved Account", ns)}
				*errors = append(*errors, err)
				continue
			}
			if _, ok := m[ns]; ok {
				err := &configErr{tk, fmt.Sprintf("Duplicate Account Entry: %s", ns)}
				*errors = append(*errors, err)
				continue
			}
			opts.Accounts = append(opts.Accounts, NewAccount(ns))
			m[ns] = struct{}{}
		}
	// More common map entry
	case map[string]interface{}:
		// Track users across accounts, must be unique across
		// accounts and nkeys vs users.
		// We also want to check for users that may have been added in
		// parseAuthorization{} if that happened first.
		uorn := setupUsersAndNKeysDuplicateCheckMap(opts)

		for aname, mv := range vv {
			tk, amv := unwrapValue(mv, &lt)

			// Skip referenced config vars within the account block.
			if tk.IsUsedVariable() {
				continue
			}

			// These should be maps.
			mv, ok := amv.(map[string]interface{})
			if !ok {
				err := &configErr{tk, "Expected map entries for accounts"}
				*errors = append(*errors, err)
				continue
			}
			if isReservedAccount(aname) {
				err := &configErr{tk, fmt.Sprintf("%q is a Reserved Account", aname)}
				*errors = append(*errors, err)
				continue
			}
			var (
				users   []*User
				nkeyUsr []*NkeyUser
				usersTk token
			)
			acc := NewAccount(aname)
			opts.Accounts = append(opts.Accounts, acc)

			for k, v := range mv {
				tk, mv := unwrapValue(v, &lt)
				switch strings.ToLower(k) {
				case "nkey":
					nk, ok := mv.(string)
					if !ok || !nkeys.IsValidPublicAccountKey(nk) {
						err := &configErr{tk, fmt.Sprintf("Not a valid public nkey for an account: %q", mv)}
						*errors = append(*errors, err)
						continue
					}
					acc.Nkey = nk
				case "imports":
					streams, services, err := parseAccountImports(tk, acc, errors, warnings)
					if err != nil {
						*errors = append(*errors, err)
						continue
					}
					importStreams = append(importStreams, streams...)
					importServices = append(importServices, services...)
				case "exports":
					streams, services, err := parseAccountExports(tk, acc, errors, warnings)
					if err != nil {
						*errors = append(*errors, err)
						continue
					}
					exportStreams = append(exportStreams, streams...)
					exportServices = append(exportServices, services...)
				case "jetstream":
					err := parseJetStreamForAccount(mv, acc, errors, warnings)
					if err != nil {
						*errors = append(*errors, err)
						continue
					}
				case "users":
					var err error
					usersTk = tk
					nkeyUsr, users, err = parseUsers(mv, opts, errors, warnings)
					if err != nil {
						*errors = append(*errors, err)
						continue
					}
				case "default_permissions":
					permissions, err := parseUserPermissions(tk, errors, warnings)
					if err != nil {
						*errors = append(*errors, err)
						continue
					}
					acc.defaultPerms = permissions
				case "mappings", "maps":
					err := parseAccountMappings(tk, acc, errors, warnings)
					if err != nil {
						*errors = append(*errors, err)
						continue
					}
				case "limits":
					err := parseAccountLimits(tk, acc, errors, warnings)
					if err != nil {
						*errors = append(*errors, err)
						continue
					}
				default:
					if !tk.IsUsedVariable() {
						err := &unknownConfigFieldErr{
							field: k,
							configErr: configErr{
								token: tk,
							},
						}
						*errors = append(*errors, err)
					}
				}
			}
			// Report error if there is an authorization{} block
			// with u/p or token and any user defined in accounts{}
			if len(nkeyUsr) > 0 || len(users) > 0 {
				if opts.Username != _EMPTY_ {
					err := &configErr{usersTk, "Can not have a single user/pass and accounts"}
					*errors = append(*errors, err)
					continue
				}
				if opts.Authorization != _EMPTY_ {
					err := &configErr{usersTk, "Can not have a token and accounts"}
					*errors = append(*errors, err)
					continue
				}
			}
			applyDefaultPermissions(users, nkeyUsr, acc.defaultPerms)
			for _, u := range nkeyUsr {
				if _, ok := uorn[u.Nkey]; ok {
					err := &configErr{usersTk, fmt.Sprintf("Duplicate nkey %q detected", u.Nkey)}
					*errors = append(*errors, err)
					continue
				}
				uorn[u.Nkey] = struct{}{}
				u.Account = acc
			}
			opts.Nkeys = append(opts.Nkeys, nkeyUsr...)
			for _, u := range users {
				if _, ok := uorn[u.Username]; ok {
					err := &configErr{usersTk, fmt.Sprintf("Duplicate user %q detected", u.Username)}
					*errors = append(*errors, err)
					continue
				}
				uorn[u.Username] = struct{}{}
				u.Account = acc
			}
			opts.Users = append(opts.Users, users...)
		}
	}
	lt = tk
	// Bail already if there are previous errors.
	if len(*errors) > 0 {
		return nil
	}

	// Parse Imports and Exports here after all accounts defined.
	// Do exports first since they need to be defined for imports to succeed
	// since we do permissions checks.

	// Create a lookup map for accounts lookups.
	am := make(map[string]*Account, len(opts.Accounts))
	for _, a := range opts.Accounts {
		am[a.Name] = a
	}
	// Do stream exports
	for _, stream := range exportStreams {
		// Make array of accounts if applicable.
		var accounts []*Account
		for _, an := range stream.accs {
			ta := am[an]
			if ta == nil {
				msg := fmt.Sprintf("%q account not defined for stream export", an)
				*errors = append(*errors, &configErr{tk, msg})
				continue
			}
			accounts = append(accounts, ta)
		}
		if err := stream.acc.addStreamExportWithAccountPos(stream.sub, accounts, stream.tPos); err != nil {
			msg := fmt.Sprintf("Error adding stream export %q: %v", stream.sub, err)
			*errors = append(*errors, &configErr{tk, msg})
			continue
		}
	}
	for _, service := range exportServices {
		// Make array of accounts if applicable.
		var accounts []*Account
		for _, an := range service.accs {
			ta := am[an]
			if ta == nil {
				msg := fmt.Sprintf("%q account not defined for service export", an)
				*errors = append(*errors, &configErr{tk, msg})
				continue
			}
			accounts = append(accounts, ta)
		}
		if err := service.acc.addServiceExportWithResponseAndAccountPos(service.sub, service.rt, accounts, service.tPos); err != nil {
			msg := fmt.Sprintf("Error adding service export %q: %v", service.sub, err)
			*errors = append(*errors, &configErr{tk, msg})
			continue
		}

		if service.rthr != 0 {
			// Response threshold was set in options.
			if err := service.acc.SetServiceExportResponseThreshold(service.sub, service.rthr); err != nil {
				msg := fmt.Sprintf("Error adding service export response threshold for %q: %v", service.sub, err)
				*errors = append(*errors, &configErr{tk, msg})
				continue
			}
		}

		if service.lat != nil {
			// System accounts are on be default so just make sure we have not opted out..
			if opts.NoSystemAccount {
				msg := fmt.Sprintf("Error adding service latency sampling for %q: %v", service.sub, ErrNoSysAccount.Error())
				*errors = append(*errors, &configErr{tk, msg})
				continue
			}

			if err := service.acc.TrackServiceExportWithSampling(service.sub, service.lat.subject, int(service.lat.sampling)); err != nil {
				msg := fmt.Sprintf("Error adding service latency sampling for %q on subject %q: %v", service.sub, service.lat.subject, err)
				*errors = append(*errors, &configErr{tk, msg})
				continue
			}
		}
	}
	for _, stream := range importStreams {
		ta := am[stream.an]
		if ta == nil {
			msg := fmt.Sprintf("%q account not defined for stream import", stream.an)
			*errors = append(*errors, &configErr{tk, msg})
			continue
		}
		if stream.pre != "" {
			if err := stream.acc.AddStreamImport(ta, stream.sub, stream.pre); err != nil {
				msg := fmt.Sprintf("Error adding stream import %q: %v", stream.sub, err)
				*errors = append(*errors, &configErr{tk, msg})
				continue
			}
		} else {
			if err := stream.acc.AddMappedStreamImport(ta, stream.sub, stream.to); err != nil {
				msg := fmt.Sprintf("Error adding stream import %q: %v", stream.sub, err)
				*errors = append(*errors, &configErr{tk, msg})
				continue
			}
		}
	}
	for _, service := range importServices {
		ta := am[service.an]
		if ta == nil {
			msg := fmt.Sprintf("%q account not defined for service import", service.an)
			*errors = append(*errors, &configErr{tk, msg})
			continue
		}
		if service.to == _EMPTY_ {
			service.to = service.sub
		}
		if err := service.acc.AddServiceImport(ta, service.to, service.sub); err != nil {
			msg := fmt.Sprintf("Error adding service import %q: %v", service.sub, err)
			*errors = append(*errors, &configErr{tk, msg})
			continue
		}
		if err := service.acc.SetServiceImportSharing(ta, service.sub, service.share); err != nil {
			msg := fmt.Sprintf("Error setting service import sharing %q: %v", service.sub, err)
			*errors = append(*errors, &configErr{tk, msg})
			continue
		}
	}

	return nil
}

// Parse the account exports
func parseAccountExports(v interface{}, acc *Account, errors, warnings *[]error) ([]*export, []*export, error) {
	var lt token
	defer convertPanicToErrorList(&lt, errors)

	// This should be an array of objects/maps.
	tk, v := unwrapValue(v, &lt)
	ims, ok := v.([]interface{})
	if !ok {
		return nil, nil, &configErr{tk, fmt.Sprintf("Exports should be an array, got %T", v)}
	}

	var services []*export
	var streams []*export

	for _, v := range ims {
		// Should have stream or service
		stream, service, err := parseExportStreamOrService(v, errors, warnings)
		if err != nil {
			*errors = append(*errors, err)
			continue
		}
		if service != nil {
			service.acc = acc
			services = append(services, service)
		}
		if stream != nil {
			stream.acc = acc
			streams = append(streams, stream)
		}
	}
	return streams, services, nil
}

// Parse the account imports
func parseAccountImports(v interface{}, acc *Account, errors, warnings *[]error) ([]*importStream, []*importService, error) {
	var lt token
	defer convertPanicToErrorList(&lt, errors)

	// This should be an array of objects/maps.
	tk, v := unwrapValue(v, &lt)
	ims, ok := v.([]interface{})
	if !ok {
		return nil, nil, &configErr{tk, fmt.Sprintf("Imports should be an array, got %T", v)}
	}

	var services []*importService
	var streams []*importStream
	svcSubjects := map[string]*importService{}

	for _, v := range ims {
		// Should have stream or service
		stream, service, err := parseImportStreamOrService(v, errors, warnings)
		if err != nil {
			*errors = append(*errors, err)
			continue
		}
		if service != nil {
			if dup := svcSubjects[service.to]; dup != nil {
				tk, _ := unwrapValue(v, &lt)
				err := &configErr{tk,
					fmt.Sprintf("Duplicate service import subject %q, previously used in import for account %q, subject %q",
						service.to, dup.an, dup.sub)}
				*errors = append(*errors, err)
				continue
			}
			svcSubjects[service.to] = service
			service.acc = acc
			services = append(services, service)
		}
		if stream != nil {
			stream.acc = acc
			streams = append(streams, stream)
		}
	}
	return streams, services, nil
}

// Helper to parse an embedded account description for imported services or streams.
func parseAccount(v map[string]interface{}, errors, warnings *[]error) (string, string, error) {
	var lt token
	defer convertPanicToErrorList(&lt, errors)

	var accountName, subject string
	for mk, mv := range v {
		tk, mv := unwrapValue(mv, &lt)
		switch strings.ToLower(mk) {
		case "account":
			accountName = mv.(string)
		case "subject":
			subject = mv.(string)
		default:
			if !tk.IsUsedVariable() {
				err := &unknownConfigFieldErr{
					field: mk,
					configErr: configErr{
						token: tk,
					},
				}
				*errors = append(*errors, err)
			}
		}
	}
	return accountName, subject, nil
}

// Parse an export stream or service.
// e.g.
// {stream: "public.>"} # No accounts means public.
// {stream: "synadia.private.>", accounts: [cncf, natsio]}
// {service: "pub.request"} # No accounts means public.
// {service: "pub.special.request", accounts: [nats.io]}
func parseExportStreamOrService(v interface{}, errors, warnings *[]error) (*export, *export, error) {
	var (
		curStream  *export
		curService *export
		accounts   []string
		rt         ServiceRespType
		rtSeen     bool
		rtToken    token
		lat        *serviceLatency
		threshSeen bool
		thresh     time.Duration
		latToken   token
		lt         token
		accTokPos  uint
	)
	defer convertPanicToErrorList(&lt, errors)

	tk, v := unwrapValue(v, &lt)
	vv, ok := v.(map[string]interface{})
	if !ok {
		return nil, nil, &configErr{tk, fmt.Sprintf("Export Items should be a map with type entry, got %T", v)}
	}
	for mk, mv := range vv {
		tk, mv := unwrapValue(mv, &lt)
		switch strings.ToLower(mk) {
		case "stream":
			if curService != nil {
				err := &configErr{tk, fmt.Sprintf("Detected stream %q but already saw a service", mv)}
				*errors = append(*errors, err)
				continue
			}
			if rtToken != nil {
				err := &configErr{rtToken, "Detected response directive on non-service"}
				*errors = append(*errors, err)
				continue
			}
			if latToken != nil {
				err := &configErr{latToken, "Detected latency directive on non-service"}
				*errors = append(*errors, err)
				continue
			}
			mvs, ok := mv.(string)
			if !ok {
				err := &configErr{tk, fmt.Sprintf("Expected stream name to be string, got %T", mv)}
				*errors = append(*errors, err)
				continue
			}
			curStream = &export{sub: mvs}
			if accounts != nil {
				curStream.accs = accounts
			}
		case "service":
			if curStream != nil {
				err := &configErr{tk, fmt.Sprintf("Detected service %q but already saw a stream", mv)}
				*errors = append(*errors, err)
				continue
			}
			mvs, ok := mv.(string)
			if !ok {
				err := &configErr{tk, fmt.Sprintf("Expected service name to be string, got %T", mv)}
				*errors = append(*errors, err)
				continue
			}
			curService = &export{sub: mvs}
			if accounts != nil {
				curService.accs = accounts
			}
			if rtSeen {
				curService.rt = rt
			}
			if lat != nil {
				curService.lat = lat
			}
			if threshSeen {
				curService.rthr = thresh
			}
		case "response", "response_type":
			if rtSeen {
				err := &configErr{tk, "Duplicate response type definition"}
				*errors = append(*errors, err)
				continue
			}
			rtSeen = true
			rtToken = tk
			mvs, ok := mv.(string)
			if !ok {
				err := &configErr{tk, fmt.Sprintf("Expected response type to be string, got %T", mv)}
				*errors = append(*errors, err)
				continue
			}
			switch strings.ToLower(mvs) {
			case "single", "singleton":
				rt = Singleton
			case "stream":
				rt = Streamed
			case "chunk", "chunked":
				rt = Chunked
			default:
				err := &configErr{tk, fmt.Sprintf("Unknown response type: %q", mvs)}
				*errors = append(*errors, err)
				continue
			}
			if curService != nil {
				curService.rt = rt
			}
			if curStream != nil {
				err := &configErr{tk, "Detected response directive on non-service"}
				*errors = append(*errors, err)
			}
		case "threshold", "response_threshold", "response_max_time", "response_time":
			if threshSeen {
				err := &configErr{tk, "Duplicate response threshold detected"}
				*errors = append(*errors, err)
				continue
			}
			threshSeen = true
			mvs, ok := mv.(string)
			if !ok {
				err := &configErr{tk, fmt.Sprintf("Expected response threshold to be a parseable time duration, got %T", mv)}
				*errors = append(*errors, err)
				continue
			}
			var err error
			thresh, err = time.ParseDuration(mvs)
			if err != nil {
				err := &configErr{tk, fmt.Sprintf("Expected response threshold to be a parseable time duration, got %q", mvs)}
				*errors = append(*errors, err)
				continue
			}
			if curService != nil {
				curService.rthr = thresh
			}
			if curStream != nil {
				err := &configErr{tk, "Detected response directive on non-service"}
				*errors = append(*errors, err)
			}
		case "accounts":
			for _, iv := range mv.([]interface{}) {
				_, mv := unwrapValue(iv, &lt)
				accounts = append(accounts, mv.(string))
			}
			if curStream != nil {
				curStream.accs = accounts
			} else if curService != nil {
				curService.accs = accounts
			}
		case "latency":
			latToken = tk
			var err error
			lat, err = parseServiceLatency(tk, mv)
			if err != nil {
				*errors = append(*errors, err)
				continue
			}
			if curStream != nil {
				err = &configErr{tk, "Detected latency directive on non-service"}
				*errors = append(*errors, err)
				continue
			}
			if curService != nil {
				curService.lat = lat
			}
		case "account_token_position":
			accTokPos = uint(mv.(int64))
		default:
			if !tk.IsUsedVariable() {
				err := &unknownConfigFieldErr{
					field: mk,
					configErr: configErr{
						token: tk,
					},
				}
				*errors = append(*errors, err)
			}
		}
	}
	if curStream != nil {
		curStream.tPos = accTokPos
	}
	if curService != nil {
		curService.tPos = accTokPos
	}
	return curStream, curService, nil
}

// parseServiceLatency returns a latency config block.
func parseServiceLatency(root token, v interface{}) (l *serviceLatency, retErr error) {
	var lt token
	defer convertPanicToError(&lt, &retErr)

	if subject, ok := v.(string); ok {
		return &serviceLatency{
			subject:  subject,
			sampling: DEFAULT_SERVICE_LATENCY_SAMPLING,
		}, nil
	}

	latency, ok := v.(map[string]interface{})
	if !ok {
		return nil, &configErr{token: root,
			reason: fmt.Sprintf("Expected latency entry to be a map/struct or string, got %T", v)}
	}

	sl := serviceLatency{
		sampling: DEFAULT_SERVICE_LATENCY_SAMPLING,
	}

	// Read sampling value.
	if v, ok := latency["sampling"]; ok {
		tk, v := unwrapValue(v, &lt)
		header := false
		var sample int64
		switch vv := v.(type) {
		case int64:
			// Sample is an int, like 50.
			sample = vv
		case string:
			// Sample is a string, like "50%".
			if strings.ToLower(strings.TrimSpace(vv)) == "headers" {
				header = true
				sample = 0
				break
			}
			s := strings.TrimSuffix(vv, "%")
			n, err := strconv.Atoi(s)
			if err != nil {
				return nil, &configErr{token: tk,
					reason: fmt.Sprintf("Failed to parse latency sample: %v", err)}
			}
			sample = int64(n)
		default:
			return nil, &configErr{token: tk,
				reason: fmt.Sprintf("Expected latency sample to be a string or map/struct, got %T", v)}
		}
		if !header {
			if sample < 1 || sample > 100 {
				return nil, &configErr{token: tk,
					reason: ErrBadSampling.Error()}
			}
		}

		sl.sampling = int8(sample)
	}

	// Read subject value.
	v, ok = latency["subject"]
	if !ok {
		return nil, &configErr{token: root,
			reason: "Latency subject required, but missing"}
	}

	tk, v := unwrapValue(v, &lt)
	subject, ok := v.(string)
	if !ok {
		return nil, &configErr{token: tk,
			reason: fmt.Sprintf("Expected latency subject to be a string, got %T", subject)}
	}
	sl.subject = subject

	return &sl, nil
}

// Parse an import stream or service.
// e.g.
// {stream: {account: "synadia", subject:"public.synadia"}, prefix: "imports.synadia"}
// {stream: {account: "synadia", subject:"synadia.private.*"}}
// {service: {account: "synadia", subject: "pub.special.request"}, to: "synadia.request"}
func parseImportStreamOrService(v interface{}, errors, warnings *[]error) (*importStream, *importService, error) {
	var (
		curStream  *importStream
		curService *importService
		pre, to    string
		share      bool
		lt         token
	)
	defer convertPanicToErrorList(&lt, errors)

	tk, mv := unwrapValue(v, &lt)
	vv, ok := mv.(map[string]interface{})
	if !ok {
		return nil, nil, &configErr{tk, fmt.Sprintf("Import Items should be a map with type entry, got %T", mv)}
	}
	for mk, mv := range vv {
		tk, mv := unwrapValue(mv, &lt)
		switch strings.ToLower(mk) {
		case "stream":
			if curService != nil {
				err := &configErr{tk, "Detected stream but already saw a service"}
				*errors = append(*errors, err)
				continue
			}
			ac, ok := mv.(map[string]interface{})
			if !ok {
				err := &configErr{tk, fmt.Sprintf("Stream entry should be an account map, got %T", mv)}
				*errors = append(*errors, err)
				continue
			}
			// Make sure this is a map with account and subject
			accountName, subject, err := parseAccount(ac, errors, warnings)
			if err != nil {
				*errors = append(*errors, err)
				continue
			}
			if accountName == "" || subject == "" {
				err := &configErr{tk, "Expect an account name and a subject"}
				*errors = append(*errors, err)
				continue
			}
			curStream = &importStream{an: accountName, sub: subject}
			if to != "" {
				curStream.to = to
			}
			if pre != "" {
				curStream.pre = pre
			}
		case "service":
			if curStream != nil {
				err := &configErr{tk, "Detected service but already saw a stream"}
				*errors = append(*errors, err)
				continue
			}
			ac, ok := mv.(map[string]interface{})
			if !ok {
				err := &configErr{tk, fmt.Sprintf("Service entry should be an account map, got %T", mv)}
				*errors = append(*errors, err)
				continue
			}
			// Make sure this is a map with account and subject
			accountName, subject, err := parseAccount(ac, errors, warnings)
			if err != nil {
				*errors = append(*errors, err)
				continue
			}
			if accountName == "" || subject == "" {
				err := &configErr{tk, "Expect an account name and a subject"}
				*errors = append(*errors, err)
				continue
			}
			curService = &importService{an: accountName, sub: subject}
			if to != "" {
				curService.to = to
			} else {
				curService.to = subject
			}
			curService.share = share
		case "prefix":
			pre = mv.(string)
			if curStream != nil {
				curStream.pre = pre
			}
		case "to":
			to = mv.(string)
			if curService != nil {
				curService.to = to
			}
			if curStream != nil {
				curStream.to = to
				if curStream.pre != "" {
					err := &configErr{tk, "Stream import can not have a 'prefix' and a 'to' property"}
					*errors = append(*errors, err)
					continue
				}
			}
		case "share":
			share = mv.(bool)
			if curService != nil {
				curService.share = share
			}
		default:
			if !tk.IsUsedVariable() {
				err := &unknownConfigFieldErr{
					field: mk,
					configErr: configErr{
						token: tk,
					},
				}
				*errors = append(*errors, err)
			}
		}

	}
	return curStream, curService, nil
}

// Apply permission defaults to users/nkeyuser that don't have their own.
func applyDefaultPermissions(users []*User, nkeys []*NkeyUser, defaultP *Permissions) {
	if defaultP == nil {
		return
	}
	for _, user := range users {
		if user.Permissions == nil {
			user.Permissions = defaultP
		}
	}
	for _, user := range nkeys {
		if user.Permissions == nil {
			user.Permissions = defaultP
		}
	}
}

// Helper function to parse Authorization configs.
func parseAuthorization(v interface{}, opts *Options, errors *[]error, warnings *[]error) (*authorization, error) {
	var (
		am   map[string]interface{}
		tk   token
		lt   token
		auth = &authorization{}
	)
	defer convertPanicToErrorList(&lt, errors)

	_, v = unwrapValue(v, &lt)
	am = v.(map[string]interface{})
	for mk, mv := range am {
		tk, mv = unwrapValue(mv, &lt)
		switch strings.ToLower(mk) {
		case "user", "username":
			auth.user = mv.(string)
		case "pass", "password":
			auth.pass = mv.(string)
		case "token":
			auth.token = mv.(string)
		case "timeout":
			at := float64(1)
			switch mv := mv.(type) {
			case int64:
				at = float64(mv)
			case float64:
				at = mv
			}
			auth.timeout = at
		case "users":
			nkeys, users, err := parseUsers(tk, opts, errors, warnings)
			if err != nil {
				*errors = append(*errors, err)
				continue
			}
			auth.users = users
			auth.nkeys = nkeys
		case "default_permission", "default_permissions", "permissions":
			permissions, err := parseUserPermissions(tk, errors, warnings)
			if err != nil {
				*errors = append(*errors, err)
				continue
			}
			auth.defaultPermissions = permissions
		default:
			if !tk.IsUsedVariable() {
				err := &unknownConfigFieldErr{
					field: mk,
					configErr: configErr{
						token: tk,
					},
				}
				*errors = append(*errors, err)
			}
			continue
		}

		applyDefaultPermissions(auth.users, auth.nkeys, auth.defaultPermissions)
	}
	return auth, nil
}

// Helper function to parse multiple users array with optional permissions.
func parseUsers(mv interface{}, opts *Options, errors *[]error, warnings *[]error) ([]*NkeyUser, []*User, error) {
	var (
		tk    token
		lt    token
		keys  []*NkeyUser
		users = []*User{}
	)
	defer convertPanicToErrorList(&lt, errors)
	tk, mv = unwrapValue(mv, &lt)

	// Make sure we have an array
	uv, ok := mv.([]interface{})
	if !ok {
		return nil, nil, &configErr{tk, fmt.Sprintf("Expected users field to be an array, got %v", mv)}
	}
	for _, u := range uv {
		tk, u = unwrapValue(u, &lt)

		// Check its a map/struct
		um, ok := u.(map[string]interface{})
		if !ok {
			err := &configErr{tk, fmt.Sprintf("Expected user entry to be a map/struct, got %v", u)}
			*errors = append(*errors, err)
			continue
		}

		var (
			user  = &User{}
			nkey  = &NkeyUser{}
			perms *Permissions
			err   error
		)
		for k, v := range um {
			// Also needs to unwrap first
			tk, v = unwrapValue(v, &lt)

			switch strings.ToLower(k) {
			case "nkey":
				nkey.Nkey = v.(string)
			case "user", "username":
				user.Username = v.(string)
			case "pass", "password":
				user.Password = v.(string)
			case "permission", "permissions", "authorization":
				perms, err = parseUserPermissions(tk, errors, warnings)
				if err != nil {
					*errors = append(*errors, err)
					continue
				}
			case "allowed_connection_types", "connection_types", "clients":
				cts := parseAllowedConnectionTypes(tk, &lt, v, errors, warnings)
				nkey.AllowedConnectionTypes = cts
				user.AllowedConnectionTypes = cts
			default:
				if !tk.IsUsedVariable() {
					err := &unknownConfigFieldErr{
						field: k,
						configErr: configErr{
							token: tk,
						},
					}
					*errors = append(*errors, err)
					continue
				}
			}
		}
		// Place perms if we have them.
		if perms != nil {
			// nkey takes precedent.
			if nkey.Nkey != "" {
				nkey.Permissions = perms
			} else {
				user.Permissions = perms
			}
		}

		// Check to make sure we have at least an nkey or username <password> defined.
		if nkey.Nkey == "" && user.Username == "" {
			return nil, nil, &configErr{tk, "User entry requires a user"}
		} else if nkey.Nkey != "" {
			// Make sure the nkey a proper public nkey for a user..
			if !nkeys.IsValidPublicUserKey(nkey.Nkey) {
				return nil, nil, &configErr{tk, "Not a valid public nkey for a user"}
			}
			// If we have user or password defined here that is an error.
			if user.Username != "" || user.Password != "" {
				return nil, nil, &configErr{tk, "Nkey users do not take usernames or passwords"}
			}
			keys = append(keys, nkey)
		} else {
			users = append(users, user)
		}
	}
	return keys, users, nil
}

func parseAllowedConnectionTypes(tk token, lt *token, mv interface{}, errors *[]error, warnings *[]error) map[string]struct{} {
	cts, err := parseStringArray("allowed connection types", tk, lt, mv, errors, warnings)
	// If error, it has already been added to the `errors` array, simply return
	if err != nil {
		return nil
	}
	m, err := convertAllowedConnectionTypes(cts)
	if err != nil {
		*errors = append(*errors, &configErr{tk, err.Error()})
	}
	return m
}

// Helper function to parse user/account permissions
func parseUserPermissions(mv interface{}, errors, warnings *[]error) (*Permissions, error) {
	var (
		tk token
		lt token
		p  = &Permissions{}
	)
	defer convertPanicToErrorList(&lt, errors)

	tk, mv = unwrapValue(mv, &lt)
	pm, ok := mv.(map[string]interface{})
	if !ok {
		return nil, &configErr{tk, fmt.Sprintf("Expected permissions to be a map/struct, got %+v", mv)}
	}
	for k, v := range pm {
		tk, mv = unwrapValue(v, &lt)

		switch strings.ToLower(k) {
		// For routes:
		// Import is Publish
		// Export is Subscribe
		case "pub", "publish", "import":
			perms, err := parseVariablePermissions(mv, errors, warnings)
			if err != nil {
				*errors = append(*errors, err)
				continue
			}
			p.Publish = perms
		case "sub", "subscribe", "export":
			perms, err := parseVariablePermissions(mv, errors, warnings)
			if err != nil {
				*errors = append(*errors, err)
				continue
			}
			p.Subscribe = perms
		case "publish_allow_responses", "allow_responses":
			rp := &ResponsePermission{
				MaxMsgs: DEFAULT_ALLOW_RESPONSE_MAX_MSGS,
				Expires: DEFAULT_ALLOW_RESPONSE_EXPIRATION,
			}
			// Try boolean first
			responses, ok := mv.(bool)
			if ok {
				if responses {
					p.Response = rp
				}
			} else {
				p.Response = parseAllowResponses(v, errors, warnings)
			}
			if p.Response != nil {
				if p.Publish == nil {
					p.Publish = &SubjectPermission{}
				}
				if p.Publish.Allow == nil {
					// We turn off the blanket allow statement.
					p.Publish.Allow = []string{}
				}
			}
		default:
			if !tk.IsUsedVariable() {
				err := &configErr{tk, fmt.Sprintf("Unknown field %q parsing permissions", k)}
				*errors = append(*errors, err)
			}
		}
	}
	return p, nil
}

// Top level parser for authorization configurations.
func parseVariablePermissions(v interface{}, errors, warnings *[]error) (*SubjectPermission, error) {
	switch vv := v.(type) {
	case map[string]interface{}:
		// New style with allow and/or deny properties.
		return parseSubjectPermission(vv, errors, warnings)
	default:
		// Old style
		return parseOldPermissionStyle(v, errors, warnings)
	}
}

// Helper function to parse subject singletons and/or arrays
func parsePermSubjects(v interface{}, errors, warnings *[]error) ([]string, error) {
	var lt token
	defer convertPanicToErrorList(&lt, errors)

	tk, v := unwrapValue(v, &lt)

	var subjects []string
	switch vv := v.(type) {
	case string:
		subjects = append(subjects, vv)
	case []string:
		subjects = vv
	case []interface{}:
		for _, i := range vv {
			tk, i := unwrapValue(i, &lt)

			subject, ok := i.(string)
			if !ok {
				return nil, &configErr{tk, "Subject in permissions array cannot be cast to string"}
			}
			subjects = append(subjects, subject)
		}
	default:
		return nil, &configErr{tk, fmt.Sprintf("Expected subject permissions to be a subject, or array of subjects, got %T", v)}
	}
	if err := checkPermSubjectArray(subjects); err != nil {
		return nil, &configErr{tk, err.Error()}
	}
	return subjects, nil
}

// Helper function to parse a ResponsePermission.
func parseAllowResponses(v interface{}, errors, warnings *[]error) *ResponsePermission {
	var lt token
	defer convertPanicToErrorList(&lt, errors)

	tk, v := unwrapValue(v, &lt)
	// Check if this is a map.
	pm, ok := v.(map[string]interface{})
	if !ok {
		err := &configErr{tk, "error parsing response permissions, expected a boolean or a map"}
		*errors = append(*errors, err)
		return nil
	}

	rp := &ResponsePermission{
		MaxMsgs: DEFAULT_ALLOW_RESPONSE_MAX_MSGS,
		Expires: DEFAULT_ALLOW_RESPONSE_EXPIRATION,
	}

	for k, v := range pm {
		tk, v = unwrapValue(v, &lt)
		switch strings.ToLower(k) {
		case "max", "max_msgs", "max_messages", "max_responses":
			max := int(v.(int64))
			// Negative values are accepted (mean infinite), and 0
			// means default value (set above).
			if max != 0 {
				rp.MaxMsgs = max
			}
		case "expires", "expiration", "ttl":
			wd, ok := v.(string)
			if ok {
				ttl, err := time.ParseDuration(wd)
				if err != nil {
					err := &configErr{tk, fmt.Sprintf("error parsing expires: %v", err)}
					*errors = append(*errors, err)
					return nil
				}
				// Negative values are accepted (mean infinite), and 0
				// means default value (set above).
				if ttl != 0 {
					rp.Expires = ttl
				}
			} else {
				err := &configErr{tk, "error parsing expires, not a duration string"}
				*errors = append(*errors, err)
				return nil
			}
		default:
			if !tk.IsUsedVariable() {
				err := &configErr{tk, fmt.Sprintf("Unknown field %q parsing permissions", k)}
				*errors = append(*errors, err)
			}
		}
	}
	return rp
}

// Helper function to parse old style authorization configs.
func parseOldPermissionStyle(v interface{}, errors, warnings *[]error) (*SubjectPermission, error) {
	subjects, err := parsePermSubjects(v, errors, warnings)
	if err != nil {
		return nil, err
	}
	return &SubjectPermission{Allow: subjects}, nil
}

// Helper function to parse new style authorization into a SubjectPermission with Allow and Deny.
func parseSubjectPermission(v interface{}, errors, warnings *[]error) (*SubjectPermission, error) {
	var lt token
	defer convertPanicToErrorList(&lt, errors)

	m := v.(map[string]interface{})
	if len(m) == 0 {
		return nil, nil
	}
	p := &SubjectPermission{}
	for k, v := range m {
		tk, _ := unwrapValue(v, &lt)
		switch strings.ToLower(k) {
		case "allow":
			subjects, err := parsePermSubjects(tk, errors, warnings)
			if err != nil {
				*errors = append(*errors, err)
				continue
			}
			p.Allow = subjects
		case "deny":
			subjects, err := parsePermSubjects(tk, errors, warnings)
			if err != nil {
				*errors = append(*errors, err)
				continue
			}
			p.Deny = subjects
		default:
			if !tk.IsUsedVariable() {
				err := &configErr{tk, fmt.Sprintf("Unknown field name %q parsing subject permissions, only 'allow' or 'deny' are permitted", k)}
				*errors = append(*errors, err)
			}
		}
	}
	return p, nil
}

// Helper function to validate permissions subjects.
func checkPermSubjectArray(sa []string) error {
	for _, s := range sa {
		if !IsValidSubject(s) {
			// Check here if this is a queue group qualified subject.
			elements := strings.Fields(s)
			if len(elements) != 2 {
				return fmt.Errorf("subject %q is not a valid subject", s)
			} else if !IsValidSubject(elements[0]) {
				return fmt.Errorf("subject %q is not a valid subject", elements[0])
			}
		}
	}
	return nil
}

// PrintTLSHelpAndDie prints TLS usage and exits.
func PrintTLSHelpAndDie() {
	fmt.Printf("%s", tlsUsage)
	for k := range cipherMap {
		fmt.Printf("    %s\n", k)
	}
	fmt.Printf("\nAvailable curve preferences include:\n")
	for k := range curvePreferenceMap {
		fmt.Printf("    %s\n", k)
	}
	os.Exit(0)
}

func parseCipher(cipherName string) (uint16, error) {
	cipher, exists := cipherMap[cipherName]
	if !exists {
		return 0, fmt.Errorf("unrecognized cipher %s", cipherName)
	}

	return cipher, nil
}

func parseCurvePreferences(curveName string) (tls.CurveID, error) {
	curve, exists := curvePreferenceMap[curveName]
	if !exists {
		return 0, fmt.Errorf("unrecognized curve preference %s", curveName)
	}
	return curve, nil
}

// Helper function to parse TLS configs.
func parseTLS(v interface{}, isClientCtx bool) (t *TLSConfigOpts, retErr error) {
	var (
		tlsm map[string]interface{}
		tc   = TLSConfigOpts{}
		lt   token
	)
	defer convertPanicToError(&lt, &retErr)

	_, v = unwrapValue(v, &lt)
	tlsm = v.(map[string]interface{})
	for mk, mv := range tlsm {
		tk, mv := unwrapValue(mv, &lt)
		switch strings.ToLower(mk) {
		case "cert_file":
			certFile, ok := mv.(string)
			if !ok {
				return nil, &configErr{tk, "error parsing tls config, expected 'cert_file' to be filename"}
			}
			tc.CertFile = certFile
		case "key_file":
			keyFile, ok := mv.(string)
			if !ok {
				return nil, &configErr{tk, "error parsing tls config, expected 'key_file' to be filename"}
			}
			tc.KeyFile = keyFile
		case "ca_file":
			caFile, ok := mv.(string)
			if !ok {
				return nil, &configErr{tk, "error parsing tls config, expected 'ca_file' to be filename"}
			}
			tc.CaFile = caFile
		case "insecure":
			insecure, ok := mv.(bool)
			if !ok {
				return nil, &configErr{tk, "error parsing tls config, expected 'insecure' to be a boolean"}
			}
			tc.Insecure = insecure
		case "verify":
			verify, ok := mv.(bool)
			if !ok {
				return nil, &configErr{tk, "error parsing tls config, expected 'verify' to be a boolean"}
			}
			tc.Verify = verify
		case "verify_and_map":
			verify, ok := mv.(bool)
			if !ok {
				return nil, &configErr{tk, "error parsing tls config, expected 'verify_and_map' to be a boolean"}
			}
			if verify {
				tc.Verify = verify
			}
			tc.Map = verify
		case "verify_cert_and_check_known_urls":
			verify, ok := mv.(bool)
			if !ok {
				return nil, &configErr{tk, "error parsing tls config, expected 'verify_cert_and_check_known_urls' to be a boolean"}
			}
			if verify && isClientCtx {
				return nil, &configErr{tk, "verify_cert_and_check_known_urls not supported in this context"}
			}
			if verify {
				tc.Verify = verify
			}
			tc.TLSCheckKnownURLs = verify
		case "cipher_suites":
			ra := mv.([]interface{})
			if len(ra) == 0 {
				return nil, &configErr{tk, "error parsing tls config, 'cipher_suites' cannot be empty"}
			}
			tc.Ciphers = make([]uint16, 0, len(ra))
			for _, r := range ra {
				tk, r := unwrapValue(r, &lt)
				cipher, err := parseCipher(r.(string))
				if err != nil {
					return nil, &configErr{tk, err.Error()}
				}
				tc.Ciphers = append(tc.Ciphers, cipher)
			}
		case "curve_preferences":
			ra := mv.([]interface{})
			if len(ra) == 0 {
				return nil, &configErr{tk, "error parsing tls config, 'curve_preferences' cannot be empty"}
			}
			tc.CurvePreferences = make([]tls.CurveID, 0, len(ra))
			for _, r := range ra {
				tk, r := unwrapValue(r, &lt)
				cps, err := parseCurvePreferences(r.(string))
				if err != nil {
					return nil, &configErr{tk, err.Error()}
				}
				tc.CurvePreferences = append(tc.CurvePreferences, cps)
			}
		case "timeout":
			at := float64(0)
			switch mv := mv.(type) {
			case int64:
				at = float64(mv)
			case float64:
				at = mv
			case string:
				d, err := time.ParseDuration(mv)
				if err != nil {
					return nil, &configErr{tk, fmt.Sprintf("error parsing tls config, 'timeout' %s", err)}
				}
				at = d.Seconds()
			default:
				return nil, &configErr{tk, "error parsing tls config, 'timeout' wrong type"}
			}
			tc.Timeout = at
		case "connection_rate_limit":
			at := int64(0)
			switch mv := mv.(type) {
			case int64:
				at = mv
			default:
				return nil, &configErr{tk, "error parsing tls config, 'connection_rate_limit' wrong type"}
			}
			tc.RateLimit = at
		case "pinned_certs":
			ra, ok := mv.([]interface{})
			if !ok {
				return nil, &configErr{tk, "error parsing tls config, expected 'pinned_certs' to be a list of hex-encoded sha256 of DER encoded SubjectPublicKeyInfo"}
			}
			if len(ra) != 0 {
				wl := PinnedCertSet{}
				re := regexp.MustCompile("^[A-Fa-f0-9]{64}$")
				for _, r := range ra {
					tk, r := unwrapValue(r, &lt)
					entry := strings.ToLower(r.(string))
					if !re.MatchString(entry) {
						return nil, &configErr{tk, fmt.Sprintf("error parsing tls config, 'pinned_certs' key %s does not look like hex-encoded sha256 of DER encoded SubjectPublicKeyInfo", entry)}
					}
					wl[entry] = struct{}{}
				}
				tc.PinnedCerts = wl
			}
		default:
			return nil, &configErr{tk, fmt.Sprintf("error parsing tls config, unknown field [%q]", mk)}
		}
	}

	// If cipher suites were not specified then use the defaults
	if tc.Ciphers == nil {
		tc.Ciphers = defaultCipherSuites()
	}

	// If curve preferences were not specified, then use the defaults
	if tc.CurvePreferences == nil {
		tc.CurvePreferences = defaultCurvePreferences()
	}

	return &tc, nil
}

func parseSimpleAuth(v interface{}, errors *[]error, warnings *[]error) *authorization {
	var (
		am   map[string]interface{}
		tk   token
		lt   token
		auth = &authorization{}
	)
	defer convertPanicToErrorList(&lt, errors)

	_, v = unwrapValue(v, &lt)
	am = v.(map[string]interface{})
	for mk, mv := range am {
		tk, mv = unwrapValue(mv, &lt)
		switch strings.ToLower(mk) {
		case "user", "username":
			auth.user = mv.(string)
		case "pass", "password":
			auth.pass = mv.(string)
		case "token":
			auth.token = mv.(string)
		case "timeout":
			at := float64(1)
			switch mv := mv.(type) {
			case int64:
				at = float64(mv)
			case float64:
				at = mv
			}
			auth.timeout = at
		default:
			if !tk.IsUsedVariable() {
				err := &unknownConfigFieldErr{
					field: mk,
					configErr: configErr{
						token: tk,
					},
				}
				*errors = append(*errors, err)
			}
			continue
		}
	}
	return auth
}

func parseStringArray(fieldName string, tk token, lt *token, mv interface{}, errors *[]error, warnings *[]error) ([]string, error) {
	switch mv := mv.(type) {
	case string:
		return []string{mv}, nil
	case []interface{}:
		strs := make([]string, 0, len(mv))
		for _, val := range mv {
			tk, val = unwrapValue(val, lt)
			if str, ok := val.(string); ok {
				strs = append(strs, str)
			} else {
				err := &configErr{tk, fmt.Sprintf("error parsing %s: unsupported type in array %T", fieldName, val)}
				*errors = append(*errors, err)
				continue
			}
		}
		return strs, nil
	default:
		err := &configErr{tk, fmt.Sprintf("error parsing %s: unsupported type %T", fieldName, mv)}
		*errors = append(*errors, err)
		return nil, err
	}
}

func parseWebsocket(v interface{}, o *Options, errors *[]error, warnings *[]error) error {
	var lt token
	defer convertPanicToErrorList(&lt, errors)

	tk, v := unwrapValue(v, &lt)
	gm, ok := v.(map[string]interface{})
	if !ok {
		return &configErr{tk, fmt.Sprintf("Expected websocket to be a map, got %T", v)}
	}
	for mk, mv := range gm {
		// Again, unwrap token value if line check is required.
		tk, mv = unwrapValue(mv, &lt)
		switch strings.ToLower(mk) {
		case "listen":
			hp, err := parseListen(mv)
			if err != nil {
				err := &configErr{tk, err.Error()}
				*errors = append(*errors, err)
				continue
			}
			o.Websocket.Host = hp.host
			o.Websocket.Port = hp.port
		case "port":
			o.Websocket.Port = int(mv.(int64))
		case "host", "net":
			o.Websocket.Host = mv.(string)
		case "advertise":
			o.Websocket.Advertise = mv.(string)
		case "no_tls":
			o.Websocket.NoTLS = mv.(bool)
		case "tls":
			tc, err := parseTLS(tk, true)
			if err != nil {
				*errors = append(*errors, err)
				continue
			}
			if o.Websocket.TLSConfig, err = GenTLSConfig(tc); err != nil {
				err := &configErr{tk, err.Error()}
				*errors = append(*errors, err)
				continue
			}
			o.Websocket.TLSMap = tc.Map
			o.Websocket.TLSPinnedCerts = tc.PinnedCerts
		case "same_origin":
			o.Websocket.SameOrigin = mv.(bool)
		case "allowed_origins", "allowed_origin", "allow_origins", "allow_origin", "origins", "origin":
			o.Websocket.AllowedOrigins, _ = parseStringArray("allowed origins", tk, &lt, mv, errors, warnings)
		case "handshake_timeout":
			ht := time.Duration(0)
			switch mv := mv.(type) {
			case int64:
				ht = time.Duration(mv) * time.Second
			case string:
				var err error
				ht, err = time.ParseDuration(mv)
				if err != nil {
					err := &configErr{tk, err.Error()}
					*errors = append(*errors, err)
					continue
				}
			default:
				err := &configErr{tk, fmt.Sprintf("error parsing handshake timeout: unsupported type %T", mv)}
				*errors = append(*errors, err)
			}
			o.Websocket.HandshakeTimeout = ht
		case "compress", "compression":
			o.Websocket.Compression = mv.(bool)
		case "authorization", "authentication":
			auth := parseSimpleAuth(tk, errors, warnings)
			o.Websocket.Username = auth.user
			o.Websocket.Password = auth.pass
			o.Websocket.Token = auth.token
			o.Websocket.AuthTimeout = auth.timeout
		case "jwt_cookie":
			o.Websocket.JWTCookie = mv.(string)
		case "no_auth_user":
			o.Websocket.NoAuthUser = mv.(string)
		default:
			if !tk.IsUsedVariable() {
				err := &unknownConfigFieldErr{
					field: mk,
					configErr: configErr{
						token: tk,
					},
				}
				*errors = append(*errors, err)
				continue
			}
		}
	}
	return nil
}

func parseMQTT(v interface{}, o *Options, errors *[]error, warnings *[]error) error {
	var lt token
	defer convertPanicToErrorList(&lt, errors)

	tk, v := unwrapValue(v, &lt)
	gm, ok := v.(map[string]interface{})
	if !ok {
		return &configErr{tk, fmt.Sprintf("Expected mqtt to be a map, got %T", v)}
	}
	for mk, mv := range gm {
		// Again, unwrap token value if line check is required.
		tk, mv = unwrapValue(mv, &lt)
		switch strings.ToLower(mk) {
		case "listen":
			hp, err := parseListen(mv)
			if err != nil {
				err := &configErr{tk, err.Error()}
				*errors = append(*errors, err)
				continue
			}
			o.MQTT.Host = hp.host
			o.MQTT.Port = hp.port
		case "port":
			o.MQTT.Port = int(mv.(int64))
		case "host", "net":
			o.MQTT.Host = mv.(string)
		case "tls":
			tc, err := parseTLS(tk, true)
			if err != nil {
				*errors = append(*errors, err)
				continue
			}
			if o.MQTT.TLSConfig, err = GenTLSConfig(tc); err != nil {
				err := &configErr{tk, err.Error()}
				*errors = append(*errors, err)
				continue
			}
			o.MQTT.TLSTimeout = tc.Timeout
			o.MQTT.TLSMap = tc.Map
			o.MQTT.TLSPinnedCerts = tc.PinnedCerts
		case "authorization", "authentication":
			auth := parseSimpleAuth(tk, errors, warnings)
			o.MQTT.Username = auth.user
			o.MQTT.Password = auth.pass
			o.MQTT.Token = auth.token
			o.MQTT.AuthTimeout = auth.timeout
		case "no_auth_user":
			o.MQTT.NoAuthUser = mv.(string)
		case "ack_wait", "ackwait":
			o.MQTT.AckWait = parseDuration("ack_wait", tk, mv, errors, warnings)
		case "max_ack_pending", "max_pending", "max_inflight":
			tmp := int(mv.(int64))
			if tmp < 0 || tmp > 0xFFFF {
				err := &configErr{tk, fmt.Sprintf("invalid value %v, should in [0..%d] range", tmp, 0xFFFF)}
				*errors = append(*errors, err)
			} else {
				o.MQTT.MaxAckPending = uint16(tmp)
			}
		case "js_domain":
			o.MQTT.JsDomain = mv.(string)
		case "stream_replicas":
			o.MQTT.StreamReplicas = int(mv.(int64))
		case "consumer_replicas":
			err := &configWarningErr{
				field: mk,
				configErr: configErr{
					token:  tk,
					reason: `consumer replicas setting ignored in this server version`,
				},
			}
			*warnings = append(*warnings, err)
		case "consumer_memory_storage":
			o.MQTT.ConsumerMemoryStorage = mv.(bool)
		case "consumer_inactive_threshold", "consumer_auto_cleanup":
			o.MQTT.ConsumerInactiveThreshold = parseDuration("consumer_inactive_threshold", tk, mv, errors, warnings)

		default:
			if !tk.IsUsedVariable() {
				err := &unknownConfigFieldErr{
					field: mk,
					configErr: configErr{
						token: tk,
					},
				}
				*errors = append(*errors, err)
				continue
			}
		}
	}
	return nil
}

// GenTLSConfig loads TLS related configuration parameters.
func GenTLSConfig(tc *TLSConfigOpts) (*tls.Config, error) {
	// Create the tls.Config from our options before including the certs.
	// It will determine the cipher suites that we prefer.
	// FIXME(dlc) change if ARM based.
	config := tls.Config{
		MinVersion:               tls.VersionTLS12,
		CipherSuites:             tc.Ciphers,
		PreferServerCipherSuites: true,
		CurvePreferences:         tc.CurvePreferences,
		InsecureSkipVerify:       tc.Insecure,
	}

	switch {
	case tc.CertFile != "" && tc.KeyFile == "":
		return nil, fmt.Errorf("missing 'key_file' in TLS configuration")
	case tc.CertFile == "" && tc.KeyFile != "":
		return nil, fmt.Errorf("missing 'cert_file' in TLS configuration")
	case tc.CertFile != "" && tc.KeyFile != "":
		// Now load in cert and private key
		cert, err := tls.LoadX509KeyPair(tc.CertFile, tc.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("error parsing X509 certificate/key pair: %v", err)
		}
		cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			return nil, fmt.Errorf("error parsing certificate: %v", err)
		}
		config.Certificates = []tls.Certificate{cert}
	}

	// Require client certificates as needed
	if tc.Verify {
		config.ClientAuth = tls.RequireAndVerifyClientCert
	}
	// Add in CAs if applicable.
	if tc.CaFile != "" {
		rootPEM, err := os.ReadFile(tc.CaFile)
		if err != nil || rootPEM == nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		ok := pool.AppendCertsFromPEM(rootPEM)
		if !ok {
			return nil, fmt.Errorf("failed to parse root ca certificate")
		}
		config.ClientCAs = pool
	}

	return &config, nil
}

// MergeOptions will merge two options giving preference to the flagOpts
// if the item is present.
func MergeOptions(fileOpts, flagOpts *Options) *Options {
	if fileOpts == nil {
		return flagOpts
	}
	if flagOpts == nil {
		return fileOpts
	}
	// Merge the two, flagOpts override
	opts := *fileOpts

	if flagOpts.Port != 0 {
		opts.Port = flagOpts.Port
	}
	if flagOpts.Host != "" {
		opts.Host = flagOpts.Host
	}
	if flagOpts.DontListen {
		opts.DontListen = flagOpts.DontListen
	}
	if flagOpts.ClientAdvertise != "" {
		opts.ClientAdvertise = flagOpts.ClientAdvertise
	}
	if flagOpts.Username != "" {
		opts.Username = flagOpts.Username
	}
	if flagOpts.Password != "" {
		opts.Password = flagOpts.Password
	}
	if flagOpts.Authorization != "" {
		opts.Authorization = flagOpts.Authorization
	}
	if flagOpts.HTTPPort != 0 {
		opts.HTTPPort = flagOpts.HTTPPort
	}
	if flagOpts.HTTPBasePath != "" {
		opts.HTTPBasePath = flagOpts.HTTPBasePath
	}
	if flagOpts.Debug {
		opts.Debug = true
	}
	if flagOpts.Trace {
		opts.Trace = true
	}
	if flagOpts.Logtime {
		opts.Logtime = true
	}
	if flagOpts.LogFile != "" {
		opts.LogFile = flagOpts.LogFile
	}
	if flagOpts.PidFile != "" {
		opts.PidFile = flagOpts.PidFile
	}
	if flagOpts.PortsFileDir != "" {
		opts.PortsFileDir = flagOpts.PortsFileDir
	}
	if flagOpts.ProfPort != 0 {
		opts.ProfPort = flagOpts.ProfPort
	}
	if flagOpts.Cluster.ListenStr != "" {
		opts.Cluster.ListenStr = flagOpts.Cluster.ListenStr
	}
	if flagOpts.Cluster.NoAdvertise {
		opts.Cluster.NoAdvertise = true
	}
	if flagOpts.Cluster.ConnectRetries != 0 {
		opts.Cluster.ConnectRetries = flagOpts.Cluster.ConnectRetries
	}
	if flagOpts.Cluster.Advertise != "" {
		opts.Cluster.Advertise = flagOpts.Cluster.Advertise
	}
	if flagOpts.RoutesStr != "" {
		mergeRoutes(&opts, flagOpts)
	}
	if flagOpts.JetStream {
		fileOpts.JetStream = flagOpts.JetStream
	}
	return &opts
}

// RoutesFromStr parses route URLs from a string
func RoutesFromStr(routesStr string) []*url.URL {
	routes := strings.Split(routesStr, ",")
	if len(routes) == 0 {
		return nil
	}
	routeUrls := []*url.URL{}
	for _, r := range routes {
		r = strings.TrimSpace(r)
		u, _ := url.Parse(r)
		routeUrls = append(routeUrls, u)
	}
	return routeUrls
}

// This will merge the flag routes and override anything that was present.
func mergeRoutes(opts, flagOpts *Options) {
	routeUrls := RoutesFromStr(flagOpts.RoutesStr)
	if routeUrls == nil {
		return
	}
	opts.Routes = routeUrls
	opts.RoutesStr = flagOpts.RoutesStr
}

// RemoveSelfReference removes this server from an array of routes
func RemoveSelfReference(clusterPort int, routes []*url.URL) ([]*url.URL, error) {
	var cleanRoutes []*url.URL
	cport := strconv.Itoa(clusterPort)

	selfIPs, err := getInterfaceIPs()
	if err != nil {
		return nil, err
	}
	for _, r := range routes {
		host, port, err := net.SplitHostPort(r.Host)
		if err != nil {
			return nil, err
		}

		ipList, err := getURLIP(host)
		if err != nil {
			return nil, err
		}
		if cport == port && isIPInList(selfIPs, ipList) {
			continue
		}
		cleanRoutes = append(cleanRoutes, r)
	}

	return cleanRoutes, nil
}

func isIPInList(list1 []net.IP, list2 []net.IP) bool {
	for _, ip1 := range list1 {
		for _, ip2 := range list2 {
			if ip1.Equal(ip2) {
				return true
			}
		}
	}
	return false
}

func getURLIP(ipStr string) ([]net.IP, error) {
	ipList := []net.IP{}

	ip := net.ParseIP(ipStr)
	if ip != nil {
		ipList = append(ipList, ip)
		return ipList, nil
	}

	hostAddr, err := net.LookupHost(ipStr)
	if err != nil {
		return nil, fmt.Errorf("Error looking up host with route hostname: %v", err)
	}
	for _, addr := range hostAddr {
		ip = net.ParseIP(addr)
		if ip != nil {
			ipList = append(ipList, ip)
		}
	}
	return ipList, nil
}

func getInterfaceIPs() ([]net.IP, error) {
	var localIPs []net.IP

	interfaceAddr, err := net.InterfaceAddrs()
	if err != nil {
		return nil, fmt.Errorf("Error getting self referencing address: %v", err)
	}

	for i := 0; i < len(interfaceAddr); i++ {
		interfaceIP, _, _ := net.ParseCIDR(interfaceAddr[i].String())
		if net.ParseIP(interfaceIP.String()) != nil {
			localIPs = append(localIPs, interfaceIP)
		} else {
			return nil, fmt.Errorf("Error parsing self referencing address: %v", err)
		}
	}
	return localIPs, nil
}

func setBaselineOptions(opts *Options) {
	// Setup non-standard Go defaults
	if opts.Host == "" {
		opts.Host = DEFAULT_HOST
	}
	if opts.HTTPHost == "" {
		// Default to same bind from server if left undefined
		opts.HTTPHost = opts.Host
	}
	if opts.Port == 0 {
		opts.Port = DEFAULT_PORT
	} else if opts.Port == RANDOM_PORT {
		// Choose randomly inside of net.Listen
		opts.Port = 0
	}
	if opts.MaxConn == 0 {
		opts.MaxConn = DEFAULT_MAX_CONNECTIONS
	}
	if opts.PingInterval == 0 {
		opts.PingInterval = DEFAULT_PING_INTERVAL
	}
	if opts.MaxPingsOut == 0 {
		opts.MaxPingsOut = DEFAULT_PING_MAX_OUT
	}
	if opts.TLSTimeout == 0 {
		opts.TLSTimeout = float64(TLS_TIMEOUT) / float64(time.Second)
	}
	if opts.AuthTimeout == 0 {
		opts.AuthTimeout = getDefaultAuthTimeout(opts.TLSConfig, opts.TLSTimeout)
	}
	if opts.Cluster.Port != 0 {
		if opts.Cluster.Host == "" {
			opts.Cluster.Host = DEFAULT_HOST
		}
		if opts.Cluster.TLSTimeout == 0 {
			opts.Cluster.TLSTimeout = float64(TLS_TIMEOUT) / float64(time.Second)
		}
		if opts.Cluster.AuthTimeout == 0 {
			opts.Cluster.AuthTimeout = getDefaultAuthTimeout(opts.Cluster.TLSConfig, opts.Cluster.TLSTimeout)
		}
	}
	if opts.LeafNode.Port != 0 {
		if opts.LeafNode.Host == "" {
			opts.LeafNode.Host = DEFAULT_HOST
		}
		if opts.LeafNode.TLSTimeout == 0 {
			opts.LeafNode.TLSTimeout = float64(TLS_TIMEOUT) / float64(time.Second)
		}
		if opts.LeafNode.AuthTimeout == 0 {
			opts.LeafNode.AuthTimeout = getDefaultAuthTimeout(opts.LeafNode.TLSConfig, opts.LeafNode.TLSTimeout)
		}
	}
	// Set baseline connect port for remotes.
	for _, r := range opts.LeafNode.Remotes {
		if r != nil {
			for _, u := range r.URLs {
				if u.Port() == "" {
					u.Host = net.JoinHostPort(u.Host, strconv.Itoa(DEFAULT_LEAFNODE_PORT))
				}
			}
		}
	}

	// Set this regardless of opts.LeafNode.Port
	if opts.LeafNode.ReconnectInterval == 0 {
		opts.LeafNode.ReconnectInterval = DEFAULT_LEAF_NODE_RECONNECT
	}

	if opts.MaxControlLine == 0 {
		opts.MaxControlLine = MAX_CONTROL_LINE_SIZE
	}
	if opts.MaxPayload == 0 {
		opts.MaxPayload = MAX_PAYLOAD_SIZE
	}
	if opts.MaxPending == 0 {
		opts.MaxPending = MAX_PENDING_SIZE
	}
	if opts.WriteDeadline == time.Duration(0) {
		opts.WriteDeadline = DEFAULT_FLUSH_DEADLINE
	}
	if opts.MaxClosedClients == 0 {
		opts.MaxClosedClients = DEFAULT_MAX_CLOSED_CLIENTS
	}
	if opts.LameDuckDuration == 0 {
		opts.LameDuckDuration = DEFAULT_LAME_DUCK_DURATION
	}
	if opts.LameDuckGracePeriod == 0 {
		opts.LameDuckGracePeriod = DEFAULT_LAME_DUCK_GRACE_PERIOD
	}
	if opts.Gateway.Port != 0 {
		if opts.Gateway.Host == "" {
			opts.Gateway.Host = DEFAULT_HOST
		}
		if opts.Gateway.TLSTimeout == 0 {
			opts.Gateway.TLSTimeout = float64(TLS_TIMEOUT) / float64(time.Second)
		}
		if opts.Gateway.AuthTimeout == 0 {
			opts.Gateway.AuthTimeout = getDefaultAuthTimeout(opts.Gateway.TLSConfig, opts.Gateway.TLSTimeout)
		}
	}
	if opts.ConnectErrorReports == 0 {
		opts.ConnectErrorReports = DEFAULT_CONNECT_ERROR_REPORTS
	}
	if opts.ReconnectErrorReports == 0 {
		opts.ReconnectErrorReports = DEFAULT_RECONNECT_ERROR_REPORTS
	}
	if opts.Websocket.Port != 0 {
		if opts.Websocket.Host == "" {
			opts.Websocket.Host = DEFAULT_HOST
		}
	}
	if opts.MQTT.Port != 0 {
		if opts.MQTT.Host == "" {
			opts.MQTT.Host = DEFAULT_HOST
		}
		if opts.MQTT.TLSTimeout == 0 {
			opts.MQTT.TLSTimeout = float64(TLS_TIMEOUT) / float64(time.Second)
		}
	}
	// JetStream
	if opts.JetStreamMaxMemory == 0 && !opts.maxMemSet {
		opts.JetStreamMaxMemory = -1
	}
	if opts.JetStreamMaxStore == 0 && !opts.maxStoreSet {
		opts.JetStreamMaxStore = -1
	}
}

func getDefaultAuthTimeout(tls *tls.Config, tlsTimeout float64) float64 {
	var authTimeout float64
	if tls != nil {
		authTimeout = tlsTimeout + 1.0
	} else {
		authTimeout = float64(AUTH_TIMEOUT / time.Second)
	}
	return authTimeout
}

// ConfigureOptions accepts a flag set and augments it with NATS Server
// specific flags. On success, an options structure is returned configured
// based on the selected flags and/or configuration file.
// The command line options take precedence to the ones in the configuration file.
func ConfigureOptions(fs *flag.FlagSet, args []string, printVersion, printHelp, printTLSHelp func()) (*Options, error) {
	opts := &Options{}
	var (
		showVersion            bool
		showHelp               bool
		showTLSHelp            bool
		signal                 string
		configFile             string
		dbgAndTrace            bool
		trcAndVerboseTrc       bool
		dbgAndTrcAndVerboseTrc bool
		err                    error
	)

	fs.BoolVar(&showHelp, "h", false, "Show this message.")
	fs.BoolVar(&showHelp, "help", false, "Show this message.")
	fs.IntVar(&opts.Port, "port", 0, "Port to listen on.")
	fs.IntVar(&opts.Port, "p", 0, "Port to listen on.")
	fs.StringVar(&opts.ServerName, "n", "", "Server name.")
	fs.StringVar(&opts.ServerName, "name", "", "Server name.")
	fs.StringVar(&opts.ServerName, "server_name", "", "Server name.")
	fs.StringVar(&opts.Host, "addr", "", "Network host to listen on.")
	fs.StringVar(&opts.Host, "a", "", "Network host to listen on.")
	fs.StringVar(&opts.Host, "net", "", "Network host to listen on.")
	fs.StringVar(&opts.ClientAdvertise, "client_advertise", "", "Client URL to advertise to other servers.")
	fs.BoolVar(&opts.Debug, "D", false, "Enable Debug logging.")
	fs.BoolVar(&opts.Debug, "debug", false, "Enable Debug logging.")
	fs.BoolVar(&opts.Trace, "V", false, "Enable Trace logging.")
	fs.BoolVar(&trcAndVerboseTrc, "VV", false, "Enable Verbose Trace logging. (Traces system account as well)")
	fs.BoolVar(&opts.Trace, "trace", false, "Enable Trace logging.")
	fs.BoolVar(&dbgAndTrace, "DV", false, "Enable Debug and Trace logging.")
	fs.BoolVar(&dbgAndTrcAndVerboseTrc, "DVV", false, "Enable Debug and Verbose Trace logging. (Traces system account as well)")
	fs.BoolVar(&opts.Logtime, "T", true, "Timestamp log entries.")
	fs.BoolVar(&opts.Logtime, "logtime", true, "Timestamp log entries.")
	fs.StringVar(&opts.Username, "user", "", "Username required for connection.")
	fs.StringVar(&opts.Password, "pass", "", "Password required for connection.")
	fs.StringVar(&opts.Authorization, "auth", "", "Authorization token required for connection.")
	fs.IntVar(&opts.HTTPPort, "m", 0, "HTTP Port for /varz, /connz endpoints.")
	fs.IntVar(&opts.HTTPPort, "http_port", 0, "HTTP Port for /varz, /connz endpoints.")
	fs.IntVar(&opts.HTTPSPort, "ms", 0, "HTTPS Port for /varz, /connz endpoints.")
	fs.IntVar(&opts.HTTPSPort, "https_port", 0, "HTTPS Port for /varz, /connz endpoints.")
	fs.StringVar(&configFile, "c", "", "Configuration file.")
	fs.StringVar(&configFile, "config", "", "Configuration file.")
	fs.BoolVar(&opts.CheckConfig, "t", false, "Check configuration and exit.")
	fs.StringVar(&signal, "sl", "", "Send signal to nats-server process (ldm, stop, quit, term, reopen, reload).")
	fs.StringVar(&signal, "signal", "", "Send signal to nats-server process (ldm, stop, quit, term, reopen, reload).")
	fs.StringVar(&opts.PidFile, "P", "", "File to store process pid.")
	fs.StringVar(&opts.PidFile, "pid", "", "File to store process pid.")
	fs.StringVar(&opts.PortsFileDir, "ports_file_dir", "", "Creates a ports file in the specified directory (<executable_name>_<pid>.ports).")
	fs.StringVar(&opts.LogFile, "l", "", "File to store logging output.")
	fs.StringVar(&opts.LogFile, "log", "", "File to store logging output.")
	fs.Int64Var(&opts.LogSizeLimit, "log_size_limit", 0, "Logfile size limit being auto-rotated")
	fs.BoolVar(&opts.Syslog, "s", false, "Enable syslog as log method.")
	fs.BoolVar(&opts.Syslog, "syslog", false, "Enable syslog as log method.")
	fs.StringVar(&opts.RemoteSyslog, "r", "", "Syslog server addr (udp://127.0.0.1:514).")
	fs.StringVar(&opts.RemoteSyslog, "remote_syslog", "", "Syslog server addr (udp://127.0.0.1:514).")
	fs.BoolVar(&showVersion, "version", false, "Print version information.")
	fs.BoolVar(&showVersion, "v", false, "Print version information.")
	fs.IntVar(&opts.ProfPort, "profile", 0, "Profiling HTTP port.")
	fs.StringVar(&opts.RoutesStr, "routes", "", "Routes to actively solicit a connection.")
	fs.StringVar(&opts.Cluster.ListenStr, "cluster", "", "Cluster url from which members can solicit routes.")
	fs.StringVar(&opts.Cluster.ListenStr, "cluster_listen", "", "Cluster url from which members can solicit routes.")
	fs.StringVar(&opts.Cluster.Advertise, "cluster_advertise", "", "Cluster URL to advertise to other servers.")
	fs.BoolVar(&opts.Cluster.NoAdvertise, "no_advertise", false, "Advertise known cluster IPs to clients.")
	fs.IntVar(&opts.Cluster.ConnectRetries, "connect_retries", 0, "For implicit routes, number of connect retries.")
	fs.StringVar(&opts.Cluster.Name, "cluster_name", "", "Cluster Name, if not set one will be dynamically generated.")
	fs.BoolVar(&showTLSHelp, "help_tls", false, "TLS help.")
	fs.BoolVar(&opts.TLS, "tls", false, "Enable TLS.")
	fs.BoolVar(&opts.TLSVerify, "tlsverify", false, "Enable TLS with client verification.")
	fs.StringVar(&opts.TLSCert, "tlscert", "", "Server certificate file.")
	fs.StringVar(&opts.TLSKey, "tlskey", "", "Private key for server certificate.")
	fs.StringVar(&opts.TLSCaCert, "tlscacert", "", "Client certificate CA for verification.")
	fs.IntVar(&opts.MaxTracedMsgLen, "max_traced_msg_len", 0, "Maximum printable length for traced messages. 0 for unlimited.")
	fs.BoolVar(&opts.JetStream, "js", false, "Enable JetStream.")
	fs.BoolVar(&opts.JetStream, "jetstream", false, "Enable JetStream.")
	fs.StringVar(&opts.StoreDir, "sd", "", "Storage directory.")
	fs.StringVar(&opts.StoreDir, "store_dir", "", "Storage directory.")

	// The flags definition above set "default" values to some of the options.
	// Calling Parse() here will override the default options with any value
	// specified from the command line. This is ok. We will then update the
	// options with the content of the configuration file (if present), and then,
	// call Parse() again to override the default+config with command line values.
	// Calling Parse() before processing config file is necessary since configFile
	// itself is a command line argument, and also Parse() is required in order
	// to know if user wants simply to show "help" or "version", etc...
	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	if showVersion {
		printVersion()
		return nil, nil
	}

	if showHelp {
		printHelp()
		return nil, nil
	}

	if showTLSHelp {
		printTLSHelp()
		return nil, nil
	}

	// Process args looking for non-flag options,
	// 'version' and 'help' only for now
	showVersion, showHelp, err = ProcessCommandLineArgs(fs)
	if err != nil {
		return nil, err
	} else if showVersion {
		printVersion()
		return nil, nil
	} else if showHelp {
		printHelp()
		return nil, nil
	}

	// Snapshot flag options.
	FlagSnapshot = opts.Clone()

	// Keep track of the boolean flags that were explicitly set with their value.
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "DVV":
			trackExplicitVal(FlagSnapshot, &FlagSnapshot.inCmdLine, "Debug", dbgAndTrcAndVerboseTrc)
			trackExplicitVal(FlagSnapshot, &FlagSnapshot.inCmdLine, "Trace", dbgAndTrcAndVerboseTrc)
			trackExplicitVal(FlagSnapshot, &FlagSnapshot.inCmdLine, "TraceVerbose", dbgAndTrcAndVerboseTrc)
		case "DV":
			trackExplicitVal(FlagSnapshot, &FlagSnapshot.inCmdLine, "Debug", dbgAndTrace)
			trackExplicitVal(FlagSnapshot, &FlagSnapshot.inCmdLine, "Trace", dbgAndTrace)
		case "D":
			fallthrough
		case "debug":
			trackExplicitVal(FlagSnapshot, &FlagSnapshot.inCmdLine, "Debug", FlagSnapshot.Debug)
		case "VV":
			trackExplicitVal(FlagSnapshot, &FlagSnapshot.inCmdLine, "Trace", trcAndVerboseTrc)
			trackExplicitVal(FlagSnapshot, &FlagSnapshot.inCmdLine, "TraceVerbose", trcAndVerboseTrc)
		case "V":
			fallthrough
		case "trace":
			trackExplicitVal(FlagSnapshot, &FlagSnapshot.inCmdLine, "Trace", FlagSnapshot.Trace)
		case "T":
			fallthrough
		case "logtime":
			trackExplicitVal(FlagSnapshot, &FlagSnapshot.inCmdLine, "Logtime", FlagSnapshot.Logtime)
		case "s":
			fallthrough
		case "syslog":
			trackExplicitVal(FlagSnapshot, &FlagSnapshot.inCmdLine, "Syslog", FlagSnapshot.Syslog)
		case "no_advertise":
			trackExplicitVal(FlagSnapshot, &FlagSnapshot.inCmdLine, "Cluster.NoAdvertise", FlagSnapshot.Cluster.NoAdvertise)
		}
	})

	// Process signal control.
	if signal != _EMPTY_ {
		if err := processSignal(signal); err != nil {
			return nil, err
		}
	}

	// Parse config if given
	if configFile != _EMPTY_ {
		// This will update the options with values from the config file.
		err := opts.ProcessConfigFile(configFile)
		if err != nil {
			if opts.CheckConfig {
				return nil, err
			}
			if cerr, ok := err.(*processConfigErr); !ok || len(cerr.Errors()) != 0 {
				return nil, err
			}
			// If we get here we only have warnings and can still continue
			fmt.Fprint(os.Stderr, err)
		} else if opts.CheckConfig {
			// Report configuration file syntax test was successful and exit.
			return opts, nil
		}

		// Call this again to override config file options with options from command line.
		// Note: We don't need to check error here since if there was an error, it would
		// have been caught the first time this function was called (after setting up the
		// flags).
		fs.Parse(args)
	} else if opts.CheckConfig {
		return nil, fmt.Errorf("must specify [-c, --config] option to check configuration file syntax")
	}

	// Special handling of some flags
	var (
		flagErr     error
		tlsDisabled bool
		tlsOverride bool
	)
	fs.Visit(func(f *flag.Flag) {
		// short-circuit if an error was encountered
		if flagErr != nil {
			return
		}
		if strings.HasPrefix(f.Name, "tls") {
			if f.Name == "tls" {
				if !opts.TLS {
					// User has specified "-tls=false", we need to disable TLS
					opts.TLSConfig = nil
					tlsDisabled = true
					tlsOverride = false
					return
				}
				tlsOverride = true
			} else if !tlsDisabled {
				tlsOverride = true
			}
		} else {
			switch f.Name {
			case "VV":
				opts.Trace, opts.TraceVerbose = trcAndVerboseTrc, trcAndVerboseTrc
			case "DVV":
				opts.Trace, opts.Debug, opts.TraceVerbose = dbgAndTrcAndVerboseTrc, dbgAndTrcAndVerboseTrc, dbgAndTrcAndVerboseTrc
			case "DV":
				// Check value to support -DV=false
				opts.Trace, opts.Debug = dbgAndTrace, dbgAndTrace
			case "cluster", "cluster_listen":
				// Override cluster config if explicitly set via flags.
				flagErr = overrideCluster(opts)
			case "routes":
				// Keep in mind that the flag has updated opts.RoutesStr at this point.
				if opts.RoutesStr == "" {
					// Set routes array to nil since routes string is empty
					opts.Routes = nil
					return
				}
				routeUrls := RoutesFromStr(opts.RoutesStr)
				opts.Routes = routeUrls
			}
		}
	})
	if flagErr != nil {
		return nil, flagErr
	}

	// This will be true if some of the `-tls` params have been set and
	// `-tls=false` has not been set.
	if tlsOverride {
		if err := overrideTLS(opts); err != nil {
			return nil, err
		}
	}

	// If we don't have cluster defined in the configuration
	// file and no cluster listen string override, but we do
	// have a routes override, we need to report misconfiguration.
	if opts.RoutesStr != "" && opts.Cluster.ListenStr == "" && opts.Cluster.Host == "" && opts.Cluster.Port == 0 {
		return nil, errors.New("solicited routes require cluster capabilities, e.g. --cluster")
	}

	return opts, nil
}

func normalizeBasePath(p string) string {
	if len(p) == 0 {
		return "/"
	}
	// add leading slash
	if p[0] != '/' {
		p = "/" + p
	}
	return path.Clean(p)
}

// overrideTLS is called when at least "-tls=true" has been set.
func overrideTLS(opts *Options) error {
	if opts.TLSCert == "" {
		return errors.New("TLS Server certificate must be present and valid")
	}
	if opts.TLSKey == "" {
		return errors.New("TLS Server private key must be present and valid")
	}

	tc := TLSConfigOpts{}
	tc.CertFile = opts.TLSCert
	tc.KeyFile = opts.TLSKey
	tc.CaFile = opts.TLSCaCert
	tc.Verify = opts.TLSVerify
	tc.Ciphers = defaultCipherSuites()

	var err error
	opts.TLSConfig, err = GenTLSConfig(&tc)
	return err
}

// overrideCluster updates Options.Cluster if that flag "cluster" (or "cluster_listen")
// has explicitly be set in the command line. If it is set to empty string, it will
// clear the Cluster options.
func overrideCluster(opts *Options) error {
	if opts.Cluster.ListenStr == "" {
		// This one is enough to disable clustering.
		opts.Cluster.Port = 0
		return nil
	}
	// -1 will fail url.Parse, so if we have -1, change it to
	// 0, and then after parse, replace the port with -1 so we get
	// automatic port allocation
	wantsRandom := false
	if strings.HasSuffix(opts.Cluster.ListenStr, ":-1") {
		wantsRandom = true
		cls := fmt.Sprintf("%s:0", opts.Cluster.ListenStr[0:len(opts.Cluster.ListenStr)-3])
		opts.Cluster.ListenStr = cls
	}
	clusterURL, err := url.Parse(opts.Cluster.ListenStr)
	if err != nil {
		return err
	}
	h, p, err := net.SplitHostPort(clusterURL.Host)
	if err != nil {
		return err
	}
	if wantsRandom {
		p = "-1"
	}
	opts.Cluster.Host = h
	_, err = fmt.Sscan(p, &opts.Cluster.Port)
	if err != nil {
		return err
	}

	if clusterURL.User != nil {
		pass, hasPassword := clusterURL.User.Password()
		if !hasPassword {
			return errors.New("expected cluster password to be set")
		}
		opts.Cluster.Password = pass

		user := clusterURL.User.Username()
		opts.Cluster.Username = user
	} else {
		// Since we override from flag and there is no user/pwd, make
		// sure we clear what we may have gotten from config file.
		opts.Cluster.Username = ""
		opts.Cluster.Password = ""
	}

	return nil
}

func processSignal(signal string) error {
	var (
		pid           string
		commandAndPid = strings.Split(signal, "=")
	)
	if l := len(commandAndPid); l == 2 {
		pid = maybeReadPidFile(commandAndPid[1])
	} else if l > 2 {
		return fmt.Errorf("invalid signal parameters: %v", commandAndPid[2:])
	}
	if err := ProcessSignal(Command(commandAndPid[0]), pid); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}

// maybeReadPidFile returns a PID or Windows service name obtained via the following method:
// 1. Try to open a file with path "pidStr" (absolute or relative).
// 2. If such a file exists and can be read, return its contents.
// 3. Otherwise, return the original "pidStr" string.
func maybeReadPidFile(pidStr string) string {
	if b, err := os.ReadFile(pidStr); err == nil {
		return string(b)
	}
	return pidStr
}

func homeDir() (string, error) {
	if runtime.GOOS == "windows" {
		homeDrive, homePath := os.Getenv("HOMEDRIVE"), os.Getenv("HOMEPATH")
		userProfile := os.Getenv("USERPROFILE")

		home := filepath.Join(homeDrive, homePath)
		if homeDrive == "" || homePath == "" {
			if userProfile == "" {
				return "", errors.New("nats: failed to get home dir, require %HOMEDRIVE% and %HOMEPATH% or %USERPROFILE%")
			}
			home = userProfile
		}

		return home, nil
	}

	home := os.Getenv("HOME")
	if home == "" {
		return "", errors.New("failed to get home dir, require $HOME")
	}
	return home, nil
}

func expandPath(p string) (string, error) {
	p = os.ExpandEnv(p)

	if !strings.HasPrefix(p, "~") {
		return p, nil
	}

	home, err := homeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, p[1:]), nil
}
