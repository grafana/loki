package sarama

import (
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rcrowley/go-metrics"
)

// Broker represents a single Kafka broker connection. All operations on this object are entirely concurrency-safe.
type Broker struct {
	conf *Config
	rack *string

	id            int32
	addr          string
	correlationID int32
	conn          net.Conn
	connErr       error
	lock          sync.Mutex
	opened        int32
	responses     chan *responsePromise
	done          chan bool

	metricRegistry             metrics.Registry
	incomingByteRate           metrics.Meter
	requestRate                metrics.Meter
	fetchRate                  metrics.Meter
	requestSize                metrics.Histogram
	requestLatency             metrics.Histogram
	outgoingByteRate           metrics.Meter
	responseRate               metrics.Meter
	responseSize               metrics.Histogram
	requestsInFlight           metrics.Counter
	protocolRequestsRate       map[int16]metrics.Meter
	brokerIncomingByteRate     metrics.Meter
	brokerRequestRate          metrics.Meter
	brokerFetchRate            metrics.Meter
	brokerRequestSize          metrics.Histogram
	brokerRequestLatency       metrics.Histogram
	brokerOutgoingByteRate     metrics.Meter
	brokerResponseRate         metrics.Meter
	brokerResponseSize         metrics.Histogram
	brokerRequestsInFlight     metrics.Counter
	brokerThrottleTime         metrics.Histogram
	brokerProtocolRequestsRate map[int16]metrics.Meter

	kerberosAuthenticator               GSSAPIKerberosAuth
	clientSessionReauthenticationTimeMs int64

	throttleTimer     *time.Timer
	throttleTimerLock sync.Mutex
}

// SASLMechanism specifies the SASL mechanism the client uses to authenticate with the broker
type SASLMechanism string

const (
	// SASLTypeOAuth represents the SASL/OAUTHBEARER mechanism (Kafka 2.0.0+)
	SASLTypeOAuth = "OAUTHBEARER"
	// SASLTypePlaintext represents the SASL/PLAIN mechanism
	SASLTypePlaintext = "PLAIN"
	// SASLTypeSCRAMSHA256 represents the SCRAM-SHA-256 mechanism.
	SASLTypeSCRAMSHA256 = "SCRAM-SHA-256"
	// SASLTypeSCRAMSHA512 represents the SCRAM-SHA-512 mechanism.
	SASLTypeSCRAMSHA512 = "SCRAM-SHA-512"
	SASLTypeGSSAPI      = "GSSAPI"
	// SASLHandshakeV0 is v0 of the Kafka SASL handshake protocol. Client and
	// server negotiate SASL auth using opaque packets.
	SASLHandshakeV0 = int16(0)
	// SASLHandshakeV1 is v1 of the Kafka SASL handshake protocol. Client and
	// server negotiate SASL by wrapping tokens with Kafka protocol headers.
	SASLHandshakeV1 = int16(1)
	// SASLExtKeyAuth is the reserved extension key name sent as part of the
	// SASL/OAUTHBEARER initial client response
	SASLExtKeyAuth = "auth"
)

// AccessToken contains an access token used to authenticate a
// SASL/OAUTHBEARER client along with associated metadata.
type AccessToken struct {
	// Token is the access token payload.
	Token string
	// Extensions is a optional map of arbitrary key-value pairs that can be
	// sent with the SASL/OAUTHBEARER initial client response. These values are
	// ignored by the SASL server if they are unexpected. This feature is only
	// supported by Kafka >= 2.1.0.
	Extensions map[string]string
}

// AccessTokenProvider is the interface that encapsulates how implementors
// can generate access tokens for Kafka broker authentication.
type AccessTokenProvider interface {
	// Token returns an access token. The implementation should ensure token
	// reuse so that multiple calls at connect time do not create multiple
	// tokens. The implementation should also periodically refresh the token in
	// order to guarantee that each call returns an unexpired token.  This
	// method should not block indefinitely--a timeout error should be returned
	// after a short period of inactivity so that the broker connection logic
	// can log debugging information and retry.
	Token() (*AccessToken, error)
}

// SCRAMClient is a an interface to a SCRAM
// client implementation.
type SCRAMClient interface {
	// Begin prepares the client for the SCRAM exchange
	// with the server with a user name and a password
	Begin(userName, password, authzID string) error
	// Step steps client through the SCRAM exchange. It is
	// called repeatedly until it errors or `Done` returns true.
	Step(challenge string) (response string, err error)
	// Done should return true when the SCRAM conversation
	// is over.
	Done() bool
}

type responsePromise struct {
	requestTime   time.Time
	correlationID int32
	headerVersion int16
	handler       func([]byte, error)
	packets       chan []byte
	errors        chan error
}

func (p *responsePromise) handle(packets []byte, err error) {
	// Use callback when provided
	if p.handler != nil {
		p.handler(packets, err)
		return
	}
	// Otherwise fallback to using channels
	if err != nil {
		p.errors <- err
		return
	}
	p.packets <- packets
}

// NewBroker creates and returns a Broker targeting the given host:port address.
// This does not attempt to actually connect, you have to call Open() for that.
func NewBroker(addr string) *Broker {
	return &Broker{id: -1, addr: addr}
}

// Open tries to connect to the Broker if it is not already connected or connecting, but does not block
// waiting for the connection to complete. This means that any subsequent operations on the broker will
// block waiting for the connection to succeed or fail. To get the effect of a fully synchronous Open call,
// follow it by a call to Connected(). The only errors Open will return directly are ConfigurationError or
// AlreadyConnected. If conf is nil, the result of NewConfig() is used.
func (b *Broker) Open(conf *Config) error {
	if !atomic.CompareAndSwapInt32(&b.opened, 0, 1) {
		return ErrAlreadyConnected
	}

	if conf == nil {
		conf = NewConfig()
	}

	err := conf.Validate()
	if err != nil {
		return err
	}

	usingApiVersionsRequests := conf.Version.IsAtLeast(V2_4_0_0) && conf.ApiVersionsRequest

	b.lock.Lock()

	if b.metricRegistry == nil {
		b.metricRegistry = newCleanupRegistry(conf.MetricRegistry)
	}

	go withRecover(func() {
		defer func() {
			b.lock.Unlock()

			// Send an ApiVersionsRequest to identify the client (KIP-511).
			// Ideally Sarama would use the response to control protocol versions,
			// but for now just fire-and-forget just to send
			if usingApiVersionsRequests {
				_, err = b.ApiVersions(&ApiVersionsRequest{
					Version:               3,
					ClientSoftwareName:    defaultClientSoftwareName,
					ClientSoftwareVersion: version(),
				})
				if err != nil {
					Logger.Printf("Error while sending ApiVersionsRequest to broker %s: %s\n", b.addr, err)
				}
			}
		}()
		dialer := conf.getDialer()
		b.conn, b.connErr = dialer.Dial("tcp", b.addr)
		if b.connErr != nil {
			Logger.Printf("Failed to connect to broker %s: %s\n", b.addr, b.connErr)
			b.conn = nil
			atomic.StoreInt32(&b.opened, 0)
			return
		}
		if conf.Net.TLS.Enable {
			b.conn = tls.Client(b.conn, validServerNameTLS(b.addr, conf.Net.TLS.Config))
		}

		b.conn = newBufConn(b.conn)
		b.conf = conf

		// Create or reuse the global metrics shared between brokers
		b.incomingByteRate = metrics.GetOrRegisterMeter("incoming-byte-rate", b.metricRegistry)
		b.requestRate = metrics.GetOrRegisterMeter("request-rate", b.metricRegistry)
		b.fetchRate = metrics.GetOrRegisterMeter("consumer-fetch-rate", b.metricRegistry)
		b.requestSize = getOrRegisterHistogram("request-size", b.metricRegistry)
		b.requestLatency = getOrRegisterHistogram("request-latency-in-ms", b.metricRegistry)
		b.outgoingByteRate = metrics.GetOrRegisterMeter("outgoing-byte-rate", b.metricRegistry)
		b.responseRate = metrics.GetOrRegisterMeter("response-rate", b.metricRegistry)
		b.responseSize = getOrRegisterHistogram("response-size", b.metricRegistry)
		b.requestsInFlight = metrics.GetOrRegisterCounter("requests-in-flight", b.metricRegistry)
		b.protocolRequestsRate = map[int16]metrics.Meter{}
		// Do not gather metrics for seeded broker (only used during bootstrap) because they share
		// the same id (-1) and are already exposed through the global metrics above
		if b.id >= 0 && !metrics.UseNilMetrics {
			b.registerMetrics()
		}

		if conf.Net.SASL.Mechanism == SASLTypeOAuth && conf.Net.SASL.Version == SASLHandshakeV0 {
			conf.Net.SASL.Version = SASLHandshakeV1
		}

		useSaslV0 := conf.Net.SASL.Version == SASLHandshakeV0 || conf.Net.SASL.Mechanism == SASLTypeGSSAPI
		if conf.Net.SASL.Enable && useSaslV0 {
			b.connErr = b.authenticateViaSASLv0()

			if b.connErr != nil {
				err = b.conn.Close()
				if err == nil {
					DebugLogger.Printf("Closed connection to broker %s due to SASL v0 auth error: %s\n", b.addr, b.connErr)
				} else {
					Logger.Printf("Error while closing connection to broker %s (due to SASL v0 auth error: %s): %s\n", b.addr, b.connErr, err)
				}
				b.conn = nil
				atomic.StoreInt32(&b.opened, 0)
				return
			}
		}

		b.done = make(chan bool)
		b.responses = make(chan *responsePromise, b.conf.Net.MaxOpenRequests-1)

		go withRecover(b.responseReceiver)
		if conf.Net.SASL.Enable && !useSaslV0 {
			b.connErr = b.authenticateViaSASLv1()
			if b.connErr != nil {
				close(b.responses)
				<-b.done
				err = b.conn.Close()
				if err == nil {
					DebugLogger.Printf("Closed connection to broker %s due to SASL v1 auth error: %s\n", b.addr, b.connErr)
				} else {
					Logger.Printf("Error while closing connection to broker %s (due to SASL v1 auth error: %s): %s\n", b.addr, b.connErr, err)
				}
				b.conn = nil
				atomic.StoreInt32(&b.opened, 0)
				return
			}
		}
		if b.id >= 0 {
			DebugLogger.Printf("Connected to broker at %s (registered as #%d)\n", b.addr, b.id)
		} else {
			DebugLogger.Printf("Connected to broker at %s (unregistered)\n", b.addr)
		}
	})

	return nil
}

func (b *Broker) ResponseSize() int {
	b.lock.Lock()
	defer b.lock.Unlock()

	return len(b.responses)
}

// Connected returns true if the broker is connected and false otherwise. If the broker is not
// connected but it had tried to connect, the error from that connection attempt is also returned.
func (b *Broker) Connected() (bool, error) {
	b.lock.Lock()
	defer b.lock.Unlock()

	return b.conn != nil, b.connErr
}

// TLSConnectionState returns the client's TLS connection state. The second return value is false if this is not a tls connection or the connection has not yet been established.
func (b *Broker) TLSConnectionState() (state tls.ConnectionState, ok bool) {
	b.lock.Lock()
	defer b.lock.Unlock()

	if b.conn == nil {
		return state, false
	}
	conn := b.conn
	if bconn, ok := b.conn.(*bufConn); ok {
		conn = bconn.Conn
	}
	if tc, ok := conn.(*tls.Conn); ok {
		return tc.ConnectionState(), true
	}
	return state, false
}

// Close closes the broker resources
func (b *Broker) Close() error {
	b.lock.Lock()
	defer b.lock.Unlock()

	if b.conn == nil {
		return ErrNotConnected
	}

	close(b.responses)
	<-b.done

	err := b.conn.Close()

	b.conn = nil
	b.connErr = nil
	b.done = nil
	b.responses = nil

	b.metricRegistry.UnregisterAll()

	if err == nil {
		DebugLogger.Printf("Closed connection to broker %s\n", b.addr)
	} else {
		Logger.Printf("Error while closing connection to broker %s: %s\n", b.addr, err)
	}

	atomic.StoreInt32(&b.opened, 0)

	return err
}

// ID returns the broker ID retrieved from Kafka's metadata, or -1 if that is not known.
func (b *Broker) ID() int32 {
	return b.id
}

// Addr returns the broker address as either retrieved from Kafka's metadata or passed to NewBroker.
func (b *Broker) Addr() string {
	return b.addr
}

// Rack returns the broker's rack as retrieved from Kafka's metadata or the
// empty string if it is not known.  The returned value corresponds to the
// broker's broker.rack configuration setting.  Requires protocol version to be
// at least v0.10.0.0.
func (b *Broker) Rack() string {
	if b.rack == nil {
		return ""
	}
	return *b.rack
}

// GetMetadata send a metadata request and returns a metadata response or error
func (b *Broker) GetMetadata(request *MetadataRequest) (*MetadataResponse, error) {
	response := new(MetadataResponse)
	response.Version = request.Version // Required to ensure use of the correct response header version

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// GetConsumerMetadata send a consumer metadata request and returns a consumer metadata response or error
func (b *Broker) GetConsumerMetadata(request *ConsumerMetadataRequest) (*ConsumerMetadataResponse, error) {
	response := new(ConsumerMetadataResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// FindCoordinator sends a find coordinate request and returns a response or error
func (b *Broker) FindCoordinator(request *FindCoordinatorRequest) (*FindCoordinatorResponse, error) {
	response := new(FindCoordinatorResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// GetAvailableOffsets return an offset response or error
func (b *Broker) GetAvailableOffsets(request *OffsetRequest) (*OffsetResponse, error) {
	response := new(OffsetResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// ProduceCallback function is called once the produce response has been parsed
// or could not be read.
type ProduceCallback func(*ProduceResponse, error)

// AsyncProduce sends a produce request and eventually call the provided callback
// with a produce response or an error.
//
// Waiting for the response is generally not blocking on the contrary to using Produce.
// If the maximum number of in flight request configured is reached then
// the request will be blocked till a previous response is received.
//
// When configured with RequiredAcks == NoResponse, the callback will not be invoked.
// If an error is returned because the request could not be sent then the callback
// will not be invoked either.
//
// Make sure not to Close the broker in the callback as it will lead to a deadlock.
func (b *Broker) AsyncProduce(request *ProduceRequest, cb ProduceCallback) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	needAcks := request.RequiredAcks != NoResponse
	// Use a nil promise when no acks is required
	var promise *responsePromise

	if needAcks {
		metricRegistry := b.metricRegistry

		// Create ProduceResponse early to provide the header version
		res := new(ProduceResponse)
		promise = &responsePromise{
			headerVersion: res.headerVersion(),
			// Packets will be converted to a ProduceResponse in the responseReceiver goroutine
			handler: func(packets []byte, err error) {
				if err != nil {
					// Failed request
					cb(nil, err)
					return
				}

				if err := versionedDecode(packets, res, request.version(), metricRegistry); err != nil {
					// Malformed response
					cb(nil, err)
					return
				}

				// Well-formed response
				b.handleThrottledResponse(res)
				cb(res, nil)
			},
		}
	}

	return b.sendWithPromise(request, promise)
}

// Produce returns a produce response or error
func (b *Broker) Produce(request *ProduceRequest) (*ProduceResponse, error) {
	var (
		response *ProduceResponse
		err      error
	)

	if request.RequiredAcks == NoResponse {
		err = b.sendAndReceive(request, nil)
	} else {
		response = new(ProduceResponse)
		err = b.sendAndReceive(request, response)
	}

	if err != nil {
		return nil, err
	}

	return response, nil
}

// Fetch returns a FetchResponse or error
func (b *Broker) Fetch(request *FetchRequest) (*FetchResponse, error) {
	defer func() {
		if b.fetchRate != nil {
			b.fetchRate.Mark(1)
		}
		if b.brokerFetchRate != nil {
			b.brokerFetchRate.Mark(1)
		}
	}()

	response := new(FetchResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// CommitOffset return an Offset commit response or error
func (b *Broker) CommitOffset(request *OffsetCommitRequest) (*OffsetCommitResponse, error) {
	response := new(OffsetCommitResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// FetchOffset returns an offset fetch response or error
func (b *Broker) FetchOffset(request *OffsetFetchRequest) (*OffsetFetchResponse, error) {
	response := new(OffsetFetchResponse)
	response.Version = request.Version // needed to handle the two header versions

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// JoinGroup returns a join group response or error
func (b *Broker) JoinGroup(request *JoinGroupRequest) (*JoinGroupResponse, error) {
	response := new(JoinGroupResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// SyncGroup returns a sync group response or error
func (b *Broker) SyncGroup(request *SyncGroupRequest) (*SyncGroupResponse, error) {
	response := new(SyncGroupResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// LeaveGroup return a leave group response or error
func (b *Broker) LeaveGroup(request *LeaveGroupRequest) (*LeaveGroupResponse, error) {
	response := new(LeaveGroupResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// Heartbeat returns a heartbeat response or error
func (b *Broker) Heartbeat(request *HeartbeatRequest) (*HeartbeatResponse, error) {
	response := new(HeartbeatResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// ListGroups return a list group response or error
func (b *Broker) ListGroups(request *ListGroupsRequest) (*ListGroupsResponse, error) {
	response := new(ListGroupsResponse)
	response.Version = request.Version // Required to ensure use of the correct response header version

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// DescribeGroups return describe group response or error
func (b *Broker) DescribeGroups(request *DescribeGroupsRequest) (*DescribeGroupsResponse, error) {
	response := new(DescribeGroupsResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// ApiVersions return api version response or error
func (b *Broker) ApiVersions(request *ApiVersionsRequest) (*ApiVersionsResponse, error) {
	response := new(ApiVersionsResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// CreateTopics send a create topic request and returns create topic response
func (b *Broker) CreateTopics(request *CreateTopicsRequest) (*CreateTopicsResponse, error) {
	response := new(CreateTopicsResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// DeleteTopics sends a delete topic request and returns delete topic response
func (b *Broker) DeleteTopics(request *DeleteTopicsRequest) (*DeleteTopicsResponse, error) {
	response := new(DeleteTopicsResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// CreatePartitions sends a create partition request and returns create
// partitions response or error
func (b *Broker) CreatePartitions(request *CreatePartitionsRequest) (*CreatePartitionsResponse, error) {
	response := new(CreatePartitionsResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// AlterPartitionReassignments sends a alter partition reassignments request and
// returns alter partition reassignments response
func (b *Broker) AlterPartitionReassignments(request *AlterPartitionReassignmentsRequest) (*AlterPartitionReassignmentsResponse, error) {
	response := new(AlterPartitionReassignmentsResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// ListPartitionReassignments sends a list partition reassignments request and
// returns list partition reassignments response
func (b *Broker) ListPartitionReassignments(request *ListPartitionReassignmentsRequest) (*ListPartitionReassignmentsResponse, error) {
	response := new(ListPartitionReassignmentsResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// ElectLeaders sends aa elect leaders request and returns list partitions elect result
func (b *Broker) ElectLeaders(request *ElectLeadersRequest) (*ElectLeadersResponse, error) {
	response := new(ElectLeadersResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// DeleteRecords send a request to delete records and return delete record
// response or error
func (b *Broker) DeleteRecords(request *DeleteRecordsRequest) (*DeleteRecordsResponse, error) {
	response := new(DeleteRecordsResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// DescribeAcls sends a describe acl request and returns a response or error
func (b *Broker) DescribeAcls(request *DescribeAclsRequest) (*DescribeAclsResponse, error) {
	response := new(DescribeAclsResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// CreateAcls sends a create acl request and returns a response or error
func (b *Broker) CreateAcls(request *CreateAclsRequest) (*CreateAclsResponse, error) {
	response := new(CreateAclsResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	errs := make([]error, 0)
	for _, res := range response.AclCreationResponses {
		if !errors.Is(res.Err, ErrNoError) {
			errs = append(errs, res.Err)
		}
	}

	if len(errs) > 0 {
		return response, Wrap(ErrCreateACLs, errs...)
	}

	return response, nil
}

// DeleteAcls sends a delete acl request and returns a response or error
func (b *Broker) DeleteAcls(request *DeleteAclsRequest) (*DeleteAclsResponse, error) {
	response := new(DeleteAclsResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// InitProducerID sends an init producer request and returns a response or error
func (b *Broker) InitProducerID(request *InitProducerIDRequest) (*InitProducerIDResponse, error) {
	response := new(InitProducerIDResponse)
	response.Version = request.version()

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// AddPartitionsToTxn send a request to add partition to txn and returns
// a response or error
func (b *Broker) AddPartitionsToTxn(request *AddPartitionsToTxnRequest) (*AddPartitionsToTxnResponse, error) {
	response := new(AddPartitionsToTxnResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// AddOffsetsToTxn sends a request to add offsets to txn and returns a response
// or error
func (b *Broker) AddOffsetsToTxn(request *AddOffsetsToTxnRequest) (*AddOffsetsToTxnResponse, error) {
	response := new(AddOffsetsToTxnResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// EndTxn sends a request to end txn and returns a response or error
func (b *Broker) EndTxn(request *EndTxnRequest) (*EndTxnResponse, error) {
	response := new(EndTxnResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// TxnOffsetCommit sends a request to commit transaction offsets and returns
// a response or error
func (b *Broker) TxnOffsetCommit(request *TxnOffsetCommitRequest) (*TxnOffsetCommitResponse, error) {
	response := new(TxnOffsetCommitResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// DescribeConfigs sends a request to describe config and returns a response or
// error
func (b *Broker) DescribeConfigs(request *DescribeConfigsRequest) (*DescribeConfigsResponse, error) {
	response := new(DescribeConfigsResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// AlterConfigs sends a request to alter config and return a response or error
func (b *Broker) AlterConfigs(request *AlterConfigsRequest) (*AlterConfigsResponse, error) {
	response := new(AlterConfigsResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// IncrementalAlterConfigs sends a request to incremental alter config and return a response or error
func (b *Broker) IncrementalAlterConfigs(request *IncrementalAlterConfigsRequest) (*IncrementalAlterConfigsResponse, error) {
	response := new(IncrementalAlterConfigsResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// DeleteGroups sends a request to delete groups and returns a response or error
func (b *Broker) DeleteGroups(request *DeleteGroupsRequest) (*DeleteGroupsResponse, error) {
	response := new(DeleteGroupsResponse)

	if err := b.sendAndReceive(request, response); err != nil {
		return nil, err
	}

	return response, nil
}

// DeleteOffsets sends a request to delete group offsets and returns a response or error
func (b *Broker) DeleteOffsets(request *DeleteOffsetsRequest) (*DeleteOffsetsResponse, error) {
	response := new(DeleteOffsetsResponse)

	if err := b.sendAndReceive(request, response); err != nil {
		return nil, err
	}

	return response, nil
}

// DescribeLogDirs sends a request to get the broker's log dir paths and sizes
func (b *Broker) DescribeLogDirs(request *DescribeLogDirsRequest) (*DescribeLogDirsResponse, error) {
	response := new(DescribeLogDirsResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// DescribeUserScramCredentials sends a request to get SCRAM users
func (b *Broker) DescribeUserScramCredentials(req *DescribeUserScramCredentialsRequest) (*DescribeUserScramCredentialsResponse, error) {
	res := new(DescribeUserScramCredentialsResponse)

	err := b.sendAndReceive(req, res)
	if err != nil {
		return nil, err
	}

	return res, err
}

func (b *Broker) AlterUserScramCredentials(req *AlterUserScramCredentialsRequest) (*AlterUserScramCredentialsResponse, error) {
	res := new(AlterUserScramCredentialsResponse)

	err := b.sendAndReceive(req, res)
	if err != nil {
		return nil, err
	}

	return res, nil
}

// DescribeClientQuotas sends a request to get the broker's quotas
func (b *Broker) DescribeClientQuotas(request *DescribeClientQuotasRequest) (*DescribeClientQuotasResponse, error) {
	response := new(DescribeClientQuotasResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// AlterClientQuotas sends a request to alter the broker's quotas
func (b *Broker) AlterClientQuotas(request *AlterClientQuotasRequest) (*AlterClientQuotasResponse, error) {
	response := new(AlterClientQuotasResponse)

	err := b.sendAndReceive(request, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// readFull ensures the conn ReadDeadline has been setup before making a
// call to io.ReadFull
func (b *Broker) readFull(buf []byte) (n int, err error) {
	if err := b.conn.SetReadDeadline(time.Now().Add(b.conf.Net.ReadTimeout)); err != nil {
		return 0, err
	}

	return io.ReadFull(b.conn, buf)
}

// write  ensures the conn WriteDeadline has been setup before making a
// call to conn.Write
func (b *Broker) write(buf []byte) (n int, err error) {
	if err := b.conn.SetWriteDeadline(time.Now().Add(b.conf.Net.WriteTimeout)); err != nil {
		return 0, err
	}

	return b.conn.Write(buf)
}

// b.lock must be held by caller
func (b *Broker) send(rb protocolBody, promiseResponse bool, responseHeaderVersion int16) (*responsePromise, error) {
	var promise *responsePromise
	if promiseResponse {
		// Packets or error will be sent to the following channels
		// once the response is received
		promise = makeResponsePromise(responseHeaderVersion)
	}

	if err := b.sendWithPromise(rb, promise); err != nil {
		return nil, err
	}

	return promise, nil
}

func makeResponsePromise(responseHeaderVersion int16) *responsePromise {
	promise := &responsePromise{
		headerVersion: responseHeaderVersion,
		packets:       make(chan []byte),
		errors:        make(chan error),
	}
	return promise
}

// b.lock must be held by caller
func (b *Broker) sendWithPromise(rb protocolBody, promise *responsePromise) error {
	if b.conn == nil {
		if b.connErr != nil {
			return b.connErr
		}
		return ErrNotConnected
	}

	if b.clientSessionReauthenticationTimeMs > 0 && currentUnixMilli() > b.clientSessionReauthenticationTimeMs {
		err := b.authenticateViaSASLv1()
		if err != nil {
			return err
		}
	}

	return b.sendInternal(rb, promise)
}

// b.lock must be held by caller
func (b *Broker) sendInternal(rb protocolBody, promise *responsePromise) error {
	if !b.conf.Version.IsAtLeast(rb.requiredVersion()) {
		return ErrUnsupportedVersion
	}

	req := &request{correlationID: b.correlationID, clientID: b.conf.ClientID, body: rb}
	buf, err := encode(req, b.metricRegistry)
	if err != nil {
		return err
	}

	// check and wait if throttled
	b.waitIfThrottled()

	requestTime := time.Now()
	// Will be decremented in responseReceiver (except error or request with NoResponse)
	b.addRequestInFlightMetrics(1)
	bytes, err := b.write(buf)
	b.updateOutgoingCommunicationMetrics(bytes)
	b.updateProtocolMetrics(rb)
	if err != nil {
		b.addRequestInFlightMetrics(-1)
		return err
	}
	b.correlationID++

	if promise == nil {
		// Record request latency without the response
		b.updateRequestLatencyAndInFlightMetrics(time.Since(requestTime))
		return nil
	}

	promise.requestTime = requestTime
	promise.correlationID = req.correlationID
	b.responses <- promise

	return nil
}

func (b *Broker) sendAndReceive(req protocolBody, res protocolBody) error {
	b.lock.Lock()
	defer b.lock.Unlock()
	responseHeaderVersion := int16(-1)
	if res != nil {
		responseHeaderVersion = res.headerVersion()
	}

	promise, err := b.send(req, res != nil, responseHeaderVersion)
	if err != nil {
		return err
	}

	if promise == nil {
		return nil
	}

	err = handleResponsePromise(req, res, promise, b.metricRegistry)
	if err != nil {
		return err
	}
	if res != nil {
		b.handleThrottledResponse(res)
	}
	return nil
}

func handleResponsePromise(req protocolBody, res protocolBody, promise *responsePromise, metricRegistry metrics.Registry) error {
	select {
	case buf := <-promise.packets:
		return versionedDecode(buf, res, req.version(), metricRegistry)
	case err := <-promise.errors:
		return err
	}
}

func (b *Broker) decode(pd packetDecoder, version int16) (err error) {
	b.id, err = pd.getInt32()
	if err != nil {
		return err
	}

	var host string
	if version < 9 {
		host, err = pd.getString()
	} else {
		host, err = pd.getCompactString()
	}
	if err != nil {
		return err
	}

	port, err := pd.getInt32()
	if err != nil {
		return err
	}

	if version >= 1 && version < 9 {
		b.rack, err = pd.getNullableString()
	} else if version >= 9 {
		b.rack, err = pd.getCompactNullableString()
	}
	if err != nil {
		return err
	}

	b.addr = net.JoinHostPort(host, fmt.Sprint(port))
	if _, _, err := net.SplitHostPort(b.addr); err != nil {
		return err
	}

	if version >= 9 {
		_, err := pd.getEmptyTaggedFieldArray()
		if err != nil {
			return err
		}
	}

	return nil
}

func (b *Broker) encode(pe packetEncoder, version int16) (err error) {
	host, portstr, err := net.SplitHostPort(b.addr)
	if err != nil {
		return err
	}

	port, err := strconv.ParseInt(portstr, 10, 32)
	if err != nil {
		return err
	}

	pe.putInt32(b.id)

	if version < 9 {
		err = pe.putString(host)
	} else {
		err = pe.putCompactString(host)
	}
	if err != nil {
		return err
	}

	pe.putInt32(int32(port))

	if version >= 1 {
		if version < 9 {
			err = pe.putNullableString(b.rack)
		} else {
			err = pe.putNullableCompactString(b.rack)
		}
		if err != nil {
			return err
		}
	}

	if version >= 9 {
		pe.putEmptyTaggedFieldArray()
	}

	return nil
}

func (b *Broker) responseReceiver() {
	var dead error

	for response := range b.responses {
		if dead != nil {
			// This was previously incremented in send() and
			// we are not calling updateIncomingCommunicationMetrics()
			b.addRequestInFlightMetrics(-1)
			response.handle(nil, dead)
			continue
		}

		headerLength := getHeaderLength(response.headerVersion)
		header := make([]byte, headerLength)

		bytesReadHeader, err := b.readFull(header)
		requestLatency := time.Since(response.requestTime)
		if err != nil {
			b.updateIncomingCommunicationMetrics(bytesReadHeader, requestLatency)
			dead = err
			response.handle(nil, err)
			continue
		}

		decodedHeader := responseHeader{}
		err = versionedDecode(header, &decodedHeader, response.headerVersion, b.metricRegistry)
		if err != nil {
			b.updateIncomingCommunicationMetrics(bytesReadHeader, requestLatency)
			dead = err
			response.handle(nil, err)
			continue
		}
		if decodedHeader.correlationID != response.correlationID {
			b.updateIncomingCommunicationMetrics(bytesReadHeader, requestLatency)
			// TODO if decoded ID < cur ID, discard until we catch up
			// TODO if decoded ID > cur ID, save it so when cur ID catches up we have a response
			dead = PacketDecodingError{fmt.Sprintf("correlation ID didn't match, wanted %d, got %d", response.correlationID, decodedHeader.correlationID)}
			response.handle(nil, dead)
			continue
		}

		buf := make([]byte, decodedHeader.length-int32(headerLength)+4)
		bytesReadBody, err := b.readFull(buf)
		b.updateIncomingCommunicationMetrics(bytesReadHeader+bytesReadBody, requestLatency)
		if err != nil {
			dead = err
			response.handle(nil, err)
			continue
		}

		response.handle(buf, nil)
	}
	close(b.done)
}

func getHeaderLength(headerVersion int16) int8 {
	if headerVersion < 1 {
		return 8
	} else {
		// header contains additional tagged field length (0), we don't support actual tags yet.
		return 9
	}
}

func (b *Broker) authenticateViaSASLv0() error {
	switch b.conf.Net.SASL.Mechanism {
	case SASLTypeSCRAMSHA256, SASLTypeSCRAMSHA512:
		return b.sendAndReceiveSASLSCRAMv0()
	case SASLTypeGSSAPI:
		return b.sendAndReceiveKerberos()
	default:
		return b.sendAndReceiveSASLPlainAuthV0()
	}
}

func (b *Broker) authenticateViaSASLv1() error {
	metricRegistry := b.metricRegistry
	if b.conf.Net.SASL.Handshake {
		handshakeRequest := &SaslHandshakeRequest{Mechanism: string(b.conf.Net.SASL.Mechanism), Version: b.conf.Net.SASL.Version}
		handshakeResponse := new(SaslHandshakeResponse)
		prom := makeResponsePromise(handshakeResponse.version())

		handshakeErr := b.sendInternal(handshakeRequest, prom)
		if handshakeErr != nil {
			Logger.Printf("Error while performing SASL handshake %s: %s\n", b.addr, handshakeErr)
			return handshakeErr
		}
		handshakeErr = handleResponsePromise(handshakeRequest, handshakeResponse, prom, metricRegistry)
		if handshakeErr != nil {
			Logger.Printf("Error while handling SASL handshake response %s: %s\n", b.addr, handshakeErr)
			return handshakeErr
		}

		if !errors.Is(handshakeResponse.Err, ErrNoError) {
			return handshakeResponse.Err
		}
	}

	authSendReceiver := func(authBytes []byte) (*SaslAuthenticateResponse, error) {
		authenticateRequest := b.createSaslAuthenticateRequest(authBytes)
		authenticateResponse := new(SaslAuthenticateResponse)
		prom := makeResponsePromise(authenticateResponse.version())
		authErr := b.sendInternal(authenticateRequest, prom)
		if authErr != nil {
			Logger.Printf("Error while performing SASL Auth %s\n", b.addr)
			return nil, authErr
		}
		authErr = handleResponsePromise(authenticateRequest, authenticateResponse, prom, metricRegistry)
		if authErr != nil {
			Logger.Printf("Error while performing SASL Auth %s: %s\n", b.addr, authErr)
			return nil, authErr
		}

		if !errors.Is(authenticateResponse.Err, ErrNoError) {
			var err error = authenticateResponse.Err
			if authenticateResponse.ErrorMessage != nil {
				err = Wrap(authenticateResponse.Err, errors.New(*authenticateResponse.ErrorMessage))
			}
			return nil, err
		}

		b.computeSaslSessionLifetime(authenticateResponse)
		return authenticateResponse, nil
	}

	switch b.conf.Net.SASL.Mechanism {
	case SASLTypeOAuth:
		provider := b.conf.Net.SASL.TokenProvider
		return b.sendAndReceiveSASLOAuth(authSendReceiver, provider)
	case SASLTypeSCRAMSHA256, SASLTypeSCRAMSHA512:
		return b.sendAndReceiveSASLSCRAMv1(authSendReceiver, b.conf.Net.SASL.SCRAMClientGeneratorFunc())
	default:
		return b.sendAndReceiveSASLPlainAuthV1(authSendReceiver)
	}
}

func (b *Broker) sendAndReceiveKerberos() error {
	b.kerberosAuthenticator.Config = &b.conf.Net.SASL.GSSAPI
	if b.kerberosAuthenticator.NewKerberosClientFunc == nil {
		b.kerberosAuthenticator.NewKerberosClientFunc = NewKerberosClient
	}
	return b.kerberosAuthenticator.Authorize(b)
}

func (b *Broker) sendAndReceiveSASLHandshake(saslType SASLMechanism, version int16) error {
	rb := &SaslHandshakeRequest{Mechanism: string(saslType), Version: version}

	req := &request{correlationID: b.correlationID, clientID: b.conf.ClientID, body: rb}
	buf, err := encode(req, b.metricRegistry)
	if err != nil {
		return err
	}

	requestTime := time.Now()
	// Will be decremented in updateIncomingCommunicationMetrics (except error)
	b.addRequestInFlightMetrics(1)
	bytes, err := b.write(buf)
	b.updateOutgoingCommunicationMetrics(bytes)
	if err != nil {
		b.addRequestInFlightMetrics(-1)
		Logger.Printf("Failed to send SASL handshake %s: %s\n", b.addr, err.Error())
		return err
	}
	b.correlationID++

	header := make([]byte, 8) // response header
	_, err = b.readFull(header)
	if err != nil {
		b.addRequestInFlightMetrics(-1)
		Logger.Printf("Failed to read SASL handshake header : %s\n", err.Error())
		return err
	}

	length := binary.BigEndian.Uint32(header[:4])
	payload := make([]byte, length-4)
	n, err := b.readFull(payload)
	if err != nil {
		b.addRequestInFlightMetrics(-1)
		Logger.Printf("Failed to read SASL handshake payload : %s\n", err.Error())
		return err
	}

	b.updateIncomingCommunicationMetrics(n+8, time.Since(requestTime))
	res := &SaslHandshakeResponse{}

	err = versionedDecode(payload, res, 0, b.metricRegistry)
	if err != nil {
		Logger.Printf("Failed to parse SASL handshake : %s\n", err.Error())
		return err
	}

	if !errors.Is(res.Err, ErrNoError) {
		Logger.Printf("Invalid SASL Mechanism : %s\n", res.Err.Error())
		return res.Err
	}

	DebugLogger.Print("Completed pre-auth SASL handshake. Available mechanisms: ", res.EnabledMechanisms)
	return nil
}

//
// In SASL Plain, Kafka expects the auth header to be in the following format
// Message format (from https://tools.ietf.org/html/rfc4616):
//
//   message   = [authzid] UTF8NUL authcid UTF8NUL passwd
//   authcid   = 1*SAFE ; MUST accept up to 255 octets
//   authzid   = 1*SAFE ; MUST accept up to 255 octets
//   passwd    = 1*SAFE ; MUST accept up to 255 octets
//   UTF8NUL   = %x00 ; UTF-8 encoded NUL character
//
//   SAFE      = UTF1 / UTF2 / UTF3 / UTF4
//                  ;; any UTF-8 encoded Unicode character except NUL
//
//

// Kafka 0.10.x supported SASL PLAIN/Kerberos via KAFKA-3149 (KIP-43).
// sendAndReceiveSASLPlainAuthV0 flows the v0 sasl auth NOT wrapped in the kafka protocol
//
// With SASL v0 handshake and auth then:
// When credentials are valid, Kafka returns a 4 byte array of null characters.
// When credentials are invalid, Kafka closes the connection.
func (b *Broker) sendAndReceiveSASLPlainAuthV0() error {
	// default to V0 to allow for backward compatibility when SASL is enabled
	// but not the handshake
	if b.conf.Net.SASL.Handshake {
		handshakeErr := b.sendAndReceiveSASLHandshake(SASLTypePlaintext, b.conf.Net.SASL.Version)
		if handshakeErr != nil {
			Logger.Printf("Error while performing SASL handshake %s: %s\n", b.addr, handshakeErr)
			return handshakeErr
		}
	}

	length := len(b.conf.Net.SASL.AuthIdentity) + 1 + len(b.conf.Net.SASL.User) + 1 + len(b.conf.Net.SASL.Password)
	authBytes := make([]byte, length+4) // 4 byte length header + auth data
	binary.BigEndian.PutUint32(authBytes, uint32(length))
	copy(authBytes[4:], b.conf.Net.SASL.AuthIdentity+"\x00"+b.conf.Net.SASL.User+"\x00"+b.conf.Net.SASL.Password)

	requestTime := time.Now()
	// Will be decremented in updateIncomingCommunicationMetrics (except error)
	b.addRequestInFlightMetrics(1)
	bytesWritten, err := b.write(authBytes)
	b.updateOutgoingCommunicationMetrics(bytesWritten)
	if err != nil {
		b.addRequestInFlightMetrics(-1)
		Logger.Printf("Failed to write SASL auth header to broker %s: %s\n", b.addr, err.Error())
		return err
	}

	header := make([]byte, 4)
	n, err := b.readFull(header)
	b.updateIncomingCommunicationMetrics(n, time.Since(requestTime))
	// If the credentials are valid, we would get a 4 byte response filled with null characters.
	// Otherwise, the broker closes the connection and we get an EOF
	if err != nil {
		Logger.Printf("Failed to read response while authenticating with SASL to broker %s: %s\n", b.addr, err.Error())
		return err
	}

	DebugLogger.Printf("SASL authentication successful with broker %s:%v - %v\n", b.addr, n, header)
	return nil
}

// Kafka 1.x.x onward added a SaslAuthenticate request/response message which
// wraps the SASL flow in the Kafka protocol, which allows for returning
// meaningful errors on authentication failure.
func (b *Broker) sendAndReceiveSASLPlainAuthV1(authSendReceiver func(authBytes []byte) (*SaslAuthenticateResponse, error)) error {
	authBytes := []byte(b.conf.Net.SASL.AuthIdentity + "\x00" + b.conf.Net.SASL.User + "\x00" + b.conf.Net.SASL.Password)
	_, err := authSendReceiver(authBytes)
	return err
}

func currentUnixMilli() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}

// sendAndReceiveSASLOAuth performs the authentication flow as described by KIP-255
// https://cwiki.apache.org/confluence/pages/viewpage.action?pageId=75968876
func (b *Broker) sendAndReceiveSASLOAuth(authSendReceiver func(authBytes []byte) (*SaslAuthenticateResponse, error), provider AccessTokenProvider) error {
	token, err := provider.Token()
	if err != nil {
		return err
	}

	message, err := buildClientFirstMessage(token)
	if err != nil {
		return err
	}

	res, err := authSendReceiver(message)
	if err != nil {
		return err
	}
	isChallenge := len(res.SaslAuthBytes) > 0

	if isChallenge {
		// Abort the token exchange. The broker returns the failure code.
		_, err = authSendReceiver([]byte(`\x01`))
	}
	return err
}

func (b *Broker) sendAndReceiveSASLSCRAMv0() error {
	if err := b.sendAndReceiveSASLHandshake(b.conf.Net.SASL.Mechanism, SASLHandshakeV0); err != nil {
		return err
	}

	scramClient := b.conf.Net.SASL.SCRAMClientGeneratorFunc()
	if err := scramClient.Begin(b.conf.Net.SASL.User, b.conf.Net.SASL.Password, b.conf.Net.SASL.SCRAMAuthzID); err != nil {
		return fmt.Errorf("failed to start SCRAM exchange with the server: %w", err)
	}

	msg, err := scramClient.Step("")
	if err != nil {
		return fmt.Errorf("failed to advance the SCRAM exchange: %w", err)
	}

	for !scramClient.Done() {
		requestTime := time.Now()
		// Will be decremented in updateIncomingCommunicationMetrics (except error)
		b.addRequestInFlightMetrics(1)
		length := len(msg)
		authBytes := make([]byte, length+4) // 4 byte length header + auth data
		binary.BigEndian.PutUint32(authBytes, uint32(length))
		copy(authBytes[4:], msg)
		_, err := b.write(authBytes)
		b.updateOutgoingCommunicationMetrics(length + 4)
		if err != nil {
			b.addRequestInFlightMetrics(-1)
			Logger.Printf("Failed to write SASL auth header to broker %s: %s\n", b.addr, err.Error())
			return err
		}
		b.correlationID++
		header := make([]byte, 4)
		_, err = b.readFull(header)
		if err != nil {
			b.addRequestInFlightMetrics(-1)
			Logger.Printf("Failed to read response header while authenticating with SASL to broker %s: %s\n", b.addr, err.Error())
			return err
		}
		payload := make([]byte, int32(binary.BigEndian.Uint32(header)))
		n, err := b.readFull(payload)
		if err != nil {
			b.addRequestInFlightMetrics(-1)
			Logger.Printf("Failed to read response payload while authenticating with SASL to broker %s: %s\n", b.addr, err.Error())
			return err
		}
		b.updateIncomingCommunicationMetrics(n+4, time.Since(requestTime))
		msg, err = scramClient.Step(string(payload))
		if err != nil {
			Logger.Println("SASL authentication failed", err)
			return err
		}
	}

	DebugLogger.Println("SASL authentication succeeded")
	return nil
}

func (b *Broker) sendAndReceiveSASLSCRAMv1(authSendReceiver func(authBytes []byte) (*SaslAuthenticateResponse, error), scramClient SCRAMClient) error {
	if err := scramClient.Begin(b.conf.Net.SASL.User, b.conf.Net.SASL.Password, b.conf.Net.SASL.SCRAMAuthzID); err != nil {
		return fmt.Errorf("failed to start SCRAM exchange with the server: %w", err)
	}

	msg, err := scramClient.Step("")
	if err != nil {
		return fmt.Errorf("failed to advance the SCRAM exchange: %w", err)
	}

	for !scramClient.Done() {
		res, err := authSendReceiver([]byte(msg))
		if err != nil {
			return err
		}

		msg, err = scramClient.Step(string(res.SaslAuthBytes))
		if err != nil {
			Logger.Println("SASL authentication failed", err)
			return err
		}
	}

	DebugLogger.Println("SASL authentication succeeded")

	return nil
}

func (b *Broker) createSaslAuthenticateRequest(msg []byte) *SaslAuthenticateRequest {
	authenticateRequest := SaslAuthenticateRequest{SaslAuthBytes: msg}
	if b.conf.Version.IsAtLeast(V2_2_0_0) {
		authenticateRequest.Version = 1
	}

	return &authenticateRequest
}

// Build SASL/OAUTHBEARER initial client response as described by RFC-7628
// https://tools.ietf.org/html/rfc7628
func buildClientFirstMessage(token *AccessToken) ([]byte, error) {
	var ext string

	if token.Extensions != nil && len(token.Extensions) > 0 {
		if _, ok := token.Extensions[SASLExtKeyAuth]; ok {
			return []byte{}, fmt.Errorf("the extension `%s` is invalid", SASLExtKeyAuth)
		}
		ext = "\x01" + mapToString(token.Extensions, "=", "\x01")
	}

	resp := []byte(fmt.Sprintf("n,,\x01auth=Bearer %s%s\x01\x01", token.Token, ext))

	return resp, nil
}

// mapToString returns a list of key-value pairs ordered by key.
// keyValSep separates the key from the value. elemSep separates each pair.
func mapToString(extensions map[string]string, keyValSep string, elemSep string) string {
	buf := make([]string, 0, len(extensions))

	for k, v := range extensions {
		buf = append(buf, k+keyValSep+v)
	}

	sort.Strings(buf)

	return strings.Join(buf, elemSep)
}

func (b *Broker) computeSaslSessionLifetime(res *SaslAuthenticateResponse) {
	if res.SessionLifetimeMs > 0 {
		// Follows the Java Kafka implementation from SaslClientAuthenticator.ReauthInfo#setAuthenticationEndAndSessionReauthenticationTimes
		// pick a random percentage between 85% and 95% for session re-authentication
		positiveSessionLifetimeMs := res.SessionLifetimeMs
		authenticationEndMs := currentUnixMilli()
		pctWindowFactorToTakeNetworkLatencyAndClockDriftIntoAccount := 0.85
		pctWindowJitterToAvoidReauthenticationStormAcrossManyChannelsSimultaneously := 0.10
		pctToUse := pctWindowFactorToTakeNetworkLatencyAndClockDriftIntoAccount + rand.Float64()*pctWindowJitterToAvoidReauthenticationStormAcrossManyChannelsSimultaneously
		sessionLifetimeMsToUse := int64(float64(positiveSessionLifetimeMs) * pctToUse)
		DebugLogger.Printf("Session expiration in %d ms and session re-authentication on or after %d ms", positiveSessionLifetimeMs, sessionLifetimeMsToUse)
		b.clientSessionReauthenticationTimeMs = authenticationEndMs + sessionLifetimeMsToUse
	} else {
		b.clientSessionReauthenticationTimeMs = 0
	}
}

func (b *Broker) updateIncomingCommunicationMetrics(bytes int, requestLatency time.Duration) {
	b.updateRequestLatencyAndInFlightMetrics(requestLatency)
	b.responseRate.Mark(1)

	if b.brokerResponseRate != nil {
		b.brokerResponseRate.Mark(1)
	}

	responseSize := int64(bytes)
	b.incomingByteRate.Mark(responseSize)
	if b.brokerIncomingByteRate != nil {
		b.brokerIncomingByteRate.Mark(responseSize)
	}

	b.responseSize.Update(responseSize)
	if b.brokerResponseSize != nil {
		b.brokerResponseSize.Update(responseSize)
	}
}

func (b *Broker) updateRequestLatencyAndInFlightMetrics(requestLatency time.Duration) {
	requestLatencyInMs := int64(requestLatency / time.Millisecond)
	b.requestLatency.Update(requestLatencyInMs)

	if b.brokerRequestLatency != nil {
		b.brokerRequestLatency.Update(requestLatencyInMs)
	}

	b.addRequestInFlightMetrics(-1)
}

func (b *Broker) addRequestInFlightMetrics(i int64) {
	b.requestsInFlight.Inc(i)
	if b.brokerRequestsInFlight != nil {
		b.brokerRequestsInFlight.Inc(i)
	}
}

func (b *Broker) updateOutgoingCommunicationMetrics(bytes int) {
	b.requestRate.Mark(1)
	if b.brokerRequestRate != nil {
		b.brokerRequestRate.Mark(1)
	}

	requestSize := int64(bytes)
	b.outgoingByteRate.Mark(requestSize)
	if b.brokerOutgoingByteRate != nil {
		b.brokerOutgoingByteRate.Mark(requestSize)
	}

	b.requestSize.Update(requestSize)
	if b.brokerRequestSize != nil {
		b.brokerRequestSize.Update(requestSize)
	}
}

func (b *Broker) updateProtocolMetrics(rb protocolBody) {
	protocolRequestsRate := b.protocolRequestsRate[rb.key()]
	if protocolRequestsRate == nil {
		protocolRequestsRate = metrics.GetOrRegisterMeter(fmt.Sprintf("protocol-requests-rate-%d", rb.key()), b.metricRegistry)
		b.protocolRequestsRate[rb.key()] = protocolRequestsRate
	}
	protocolRequestsRate.Mark(1)

	if b.brokerProtocolRequestsRate != nil {
		brokerProtocolRequestsRate := b.brokerProtocolRequestsRate[rb.key()]
		if brokerProtocolRequestsRate == nil {
			brokerProtocolRequestsRate = b.registerMeter(fmt.Sprintf("protocol-requests-rate-%d", rb.key()))
			b.brokerProtocolRequestsRate[rb.key()] = brokerProtocolRequestsRate
		}
		brokerProtocolRequestsRate.Mark(1)
	}
}

type throttleSupport interface {
	throttleTime() time.Duration
}

func (b *Broker) handleThrottledResponse(resp protocolBody) {
	throttledResponse, ok := resp.(throttleSupport)
	if !ok {
		return
	}
	throttleTime := throttledResponse.throttleTime()
	if throttleTime == time.Duration(0) {
		return
	}
	DebugLogger.Printf(
		"broker/%d %T throttled %v\n", b.ID(), resp, throttleTime)
	b.setThrottle(throttleTime)
	b.updateThrottleMetric(throttleTime)
}

func (b *Broker) setThrottle(throttleTime time.Duration) {
	b.throttleTimerLock.Lock()
	defer b.throttleTimerLock.Unlock()
	if b.throttleTimer != nil {
		// if there is an existing timer stop/clear it
		if !b.throttleTimer.Stop() {
			<-b.throttleTimer.C
		}
	}
	b.throttleTimer = time.NewTimer(throttleTime)
}

func (b *Broker) waitIfThrottled() {
	b.throttleTimerLock.Lock()
	defer b.throttleTimerLock.Unlock()
	if b.throttleTimer != nil {
		DebugLogger.Printf("broker/%d waiting for throttle timer\n", b.ID())
		<-b.throttleTimer.C
		b.throttleTimer = nil
	}
}

func (b *Broker) updateThrottleMetric(throttleTime time.Duration) {
	if b.brokerThrottleTime != nil {
		throttleTimeInMs := int64(throttleTime / time.Millisecond)
		b.brokerThrottleTime.Update(throttleTimeInMs)
	}
}

func (b *Broker) registerMetrics() {
	b.brokerIncomingByteRate = b.registerMeter("incoming-byte-rate")
	b.brokerRequestRate = b.registerMeter("request-rate")
	b.brokerFetchRate = b.registerMeter("consumer-fetch-rate")
	b.brokerRequestSize = b.registerHistogram("request-size")
	b.brokerRequestLatency = b.registerHistogram("request-latency-in-ms")
	b.brokerOutgoingByteRate = b.registerMeter("outgoing-byte-rate")
	b.brokerResponseRate = b.registerMeter("response-rate")
	b.brokerResponseSize = b.registerHistogram("response-size")
	b.brokerRequestsInFlight = b.registerCounter("requests-in-flight")
	b.brokerThrottleTime = b.registerHistogram("throttle-time-in-ms")
	b.brokerProtocolRequestsRate = map[int16]metrics.Meter{}
}

func (b *Broker) registerMeter(name string) metrics.Meter {
	nameForBroker := getMetricNameForBroker(name, b)
	return metrics.GetOrRegisterMeter(nameForBroker, b.metricRegistry)
}

func (b *Broker) registerHistogram(name string) metrics.Histogram {
	nameForBroker := getMetricNameForBroker(name, b)
	return getOrRegisterHistogram(nameForBroker, b.metricRegistry)
}

func (b *Broker) registerCounter(name string) metrics.Counter {
	nameForBroker := getMetricNameForBroker(name, b)
	return metrics.GetOrRegisterCounter(nameForBroker, b.metricRegistry)
}

func validServerNameTLS(addr string, cfg *tls.Config) *tls.Config {
	if cfg == nil {
		cfg = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}
	if cfg.ServerName != "" {
		return cfg
	}

	c := cfg.Clone()
	sn, _, err := net.SplitHostPort(addr)
	if err != nil {
		Logger.Println(fmt.Errorf("failed to get ServerName from addr %w", err))
	}
	c.ServerName = sn
	return c
}
