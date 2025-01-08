package kgo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
)

func isRetryableBrokerErr(err error) bool {
	// The error could be nil if we are evaluating multiple errors at once,
	// and only one is non-nil. The intent of this function is to evaluate
	// whether an **error** is retryable, not a non-error. We return that
	// nil is not retryable -- the calling code evaluating multiple errors
	// at once would not call into this function if all errors were nil.
	if err == nil {
		return false
	}
	// https://github.com/golang/go/issues/45729
	//
	// Temporary is relatively useless. We will still check for the
	// temporary interface, and in all cases, even with timeouts, we want
	// to retry.
	//
	// More generally, we will retry for any error that unwraps into an
	// os.SyscallError. Looking at Go's net package, the error we care
	// about is net.OpError. Looking into that further, any error that
	// reaches into the operating system return a syscall error, which is
	// then put in net.OpError's Err field as an os.SyscallError. There are
	// a few non-os.SyscallError errors, these are where Go itself detects
	// a hard failure. We do not retry those.
	//
	// We blanket retry os.SyscallError because a lot of the times, what
	// appears as a hard failure can actually be retried. For example, a
	// failed dial can be retried, maybe the resolver temporarily had a
	// problem.
	//
	// We favor testing os.SyscallError first, because net.OpError _always_
	// implements Temporary, so if we test that first, it'll return false
	// in many cases when we want to return true from os.SyscallError.
	if se := (*os.SyscallError)(nil); errors.As(err, &se) {
		// If a dial fails, potentially we could retry if the resolver
		// had a temporary hiccup, but we will err on the side of this
		// being a slightly less temporary error.
		return !isDialNonTimeoutErr(err)
	}
	// EOF can be returned if a broker kills a connection unexpectedly, and
	// we can retry that. Same for ErrClosed.
	if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) {
		return true
	}
	// We could have a retryable producer ID failure, which then bubbled up
	// as errProducerIDLoadFail so as to be retried later.
	if pe := (*errProducerIDLoadFail)(nil); errors.As(err, &pe) {
		return true
	}
	// We could have chosen a broker, and then a concurrent metadata update
	// could have removed it.
	if errors.Is(err, errChosenBrokerDead) {
		return true
	}
	// A broker kept giving us short sasl lifetimes, so we killed the
	// connection ourselves. We can retry on a new connection.
	if errors.Is(err, errSaslReauthLoop) {
		return true
	}
	// We really should not get correlation mismatch, but if we do, we can
	// retry.
	if errors.Is(err, errCorrelationIDMismatch) {
		return true
	}
	// We sometimes load the controller before issuing requests, and the
	// cluster may not yet be ready and will return -1 for the controller.
	// We can backoff and retry and hope the cluster has stabilized.
	if ce := (*errUnknownController)(nil); errors.As(err, &ce) {
		return true
	}
	// Same thought for a non-existing coordinator.
	if ce := (*errUnknownCoordinator)(nil); errors.As(err, &ce) {
		return true
	}
	var tempErr interface{ Temporary() bool }
	if errors.As(err, &tempErr) {
		return tempErr.Temporary()
	}
	return false
}

func isDialNonTimeoutErr(err error) bool {
	var ne *net.OpError
	return errors.As(err, &ne) && ne.Op == "dial" && !ne.Timeout()
}

func isAnyDialErr(err error) bool {
	var ne *net.OpError
	return errors.As(err, &ne) && ne.Op == "dial"
}

func isContextErr(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func isSkippableBrokerErr(err error) bool {
	// Some broker errors are not retryable for the given broker itself,
	// but we *could* skip the broker and try again on the next broker. For
	// example, if the user input an invalid address and a valid address
	// for seeds, when we fail dialing the first seed, we cannot retry that
	// broker, but we can skip to the next.
	//
	// We take anything that returns an OpError that *is not* a context
	// error deep inside.
	if errors.Is(err, errUnknownBroker) {
		return true
	}
	var ne *net.OpError
	if errors.As(err, &ne) && !isContextErr(err) {
		return true
	}
	return false
}

var (
	//////////////
	// INTERNAL // -- when used multiple times or checked in different areas of the client
	//////////////

	// Returned when issuing a request to a broker that the client does not
	// know about (maybe missing from metadata responses now).
	errUnknownBroker = errors.New("unknown broker")

	// A temporary error returned when a broker chosen for a request is
	// stopped due to a concurrent metadata response.
	errChosenBrokerDead = errors.New("the internal broker struct chosen to issue this request has died--either the broker id is migrating or no longer exists")

	// If a broker repeatedly gives us tiny sasl lifetimes, we fail a
	// request after a few tries to forcefully kill the connection and
	// restart a new connection ourselves.
	errSaslReauthLoop = errors.New("the broker is repeatedly giving us sasl lifetimes that are too short to write a request")

	// A temporary error returned when Kafka replies with a different
	// correlation ID than we were expecting for the request the client
	// issued.
	//
	// If this error happens, the client closes the broker connection.
	errCorrelationIDMismatch = errors.New("correlation ID mismatch")

	// Returned when using a kmsg.Request with a key larger than kmsg.MaxKey.
	errUnknownRequestKey = errors.New("request key is unknown")

	// Returned if a connection has loaded broker ApiVersions and knows
	// that the broker cannot handle the request to-be-issued request.
	errBrokerTooOld = errors.New("broker is too old; the broker has already indicated it will not know how to handle the request")

	// Returned when trying to call group functions when the client is not
	// assigned a group.
	errNotGroup = errors.New("invalid group function call when not assigned a group")

	// Returned when trying to begin a transaction with a client that does
	// not have a transactional ID.
	errNotTransactional = errors.New("invalid attempt to begin a transaction with a non-transactional client")

	// Returned when trying to produce a record outside of a transaction.
	errNotInTransaction = errors.New("cannot produce record transactionally if not in a transaction")

	errNoTopic = errors.New("cannot produce record with no topic and no default topic")

	// Returned for all buffered produce records when a user purges topics.
	errPurged = errors.New("topic purged while buffered")

	errMissingMetadataPartition = errors.New("metadata update is missing a partition that we were previously using")

	errNoCommittedOffset = errors.New("partition has no prior committed offset")

	//////////////
	// EXTERNAL //
	//////////////

	// ErrRecordTimeout is passed to produce promises when records are
	// unable to be produced within the RecordDeliveryTimeout.
	ErrRecordTimeout = errors.New("records have timed out before they were able to be produced")

	// ErrRecordRetries is passed to produce promises when records are
	// unable to be produced after RecordRetries attempts.
	ErrRecordRetries = errors.New("record failed after being retried too many times")

	// ErrMaxBuffered is returned when the maximum amount of records are
	// buffered and either manual flushing is enabled or you are using
	// TryProduce.
	ErrMaxBuffered = errors.New("the maximum amount of records are buffered, cannot buffer more")

	// ErrAborting is returned for all buffered records while
	// AbortBufferedRecords is being called.
	ErrAborting = errors.New("client is aborting buffered records")

	// ErrClientClosed is returned in various places when the client's
	// Close function has been called.
	//
	// For producing, records are failed with this error.
	//
	// For consuming, a fake partition is injected into a poll response
	// that has this error.
	//
	// For any request, the request is failed with this error.
	ErrClientClosed = errors.New("client closed")
)

// ErrFirstReadEOF is returned for responses that immediately error with
// io.EOF. This is the client's guess as to why a read from a broker is
// failing with io.EOF. Two cases are currently handled,
//
//   - When the client is using TLS but brokers are not, brokers close
//     connections immediately because the incoming request looks wrong.
//   - When SASL is required but missing, brokers close connections immediately.
//
// There may be other reasons that an immediate io.EOF is encountered (perhaps
// the connection truly was severed before a response was received), but this
// error can help you quickly check common problems.
type ErrFirstReadEOF struct {
	kind uint8
	err  error
}

type errProducerIDLoadFail struct {
	err error
}

func (e *errProducerIDLoadFail) Error() string {
	if e.err == nil {
		return "unable to initialize a producer ID due to request failures"
	}
	return fmt.Sprintf("unable to initialize a producer ID due to request failures: %v", e.err)
}

func (e *errProducerIDLoadFail) Unwrap() error { return e.err }

const (
	firstReadSASL uint8 = iota
	firstReadTLS
)

func (e *ErrFirstReadEOF) Error() string {
	switch e.kind {
	case firstReadTLS:
		return "broker closed the connection immediately after a dial, which happens if the client is using TLS when the broker is not expecting it: is TLS misconfigured on the client or the broker?"
	default: // firstReadSASL
		return "broker closed the connection immediately after a request was issued, which happens when SASL is required but not provided: is SASL missing?"
	}
}

// Unwrap returns io.EOF (or, if a custom dialer returned a wrapped io.EOF,
// this returns the custom dialer's wrapped error).
func (e *ErrFirstReadEOF) Unwrap() error { return e.err }

// ErrDataLoss is returned for Kafka >=2.1 when data loss is detected and the
// client is able to reset to the last valid offset.
type ErrDataLoss struct {
	// Topic is the topic data loss was detected on.
	Topic string
	// Partition is the partition data loss was detected on.
	Partition int32
	// ConsumedTo is what the client had consumed to for this partition before
	// data loss was detected.
	ConsumedTo int64
	// ResetTo is what the client reset the partition to; everything from
	// ResetTo to ConsumedTo was lost.
	ResetTo int64
}

func (e *ErrDataLoss) Error() string {
	return fmt.Sprintf("topic %s partition %d lost records;"+
		" the client consumed to offset %d but was reset to offset %d",
		e.Topic, e.Partition, e.ConsumedTo, e.ResetTo)
}

type errUnknownController struct {
	id int32
}

func (e *errUnknownController) Error() string {
	if e.id == -1 {
		return "broker replied that the controller broker is not available"
	}
	return fmt.Sprintf("broker replied that the controller broker is %d,"+
		" but did not reply with that broker in the broker list", e.id)
}

type errUnknownCoordinator struct {
	coordinator int32
	key         coordinatorKey
}

func (e *errUnknownCoordinator) Error() string {
	switch e.key.typ {
	case coordinatorTypeGroup:
		return fmt.Sprintf("broker replied that group %s has broker coordinator %d,"+
			" but did not reply with that broker in the broker list",
			e.key.name, e.coordinator)
	case coordinatorTypeTxn:
		return fmt.Sprintf("broker replied that txn id %s has broker coordinator %d,"+
			" but did not reply with that broker in the broker list",
			e.key.name, e.coordinator)
	default:
		return fmt.Sprintf("broker replied to an unknown coordinator key %s (type %d) that it has a broker coordinator %d,"+
			" but did not reply with that broker in the broker list", e.key.name, e.key.typ, e.coordinator)
	}
}

// ErrGroupSession is injected into a poll if an error occurred such that your
// consumer group member was kicked from the group or was never able to join
// the group.
type ErrGroupSession struct {
	Err error
}

func (e *ErrGroupSession) Error() string {
	return fmt.Sprintf("unable to join group session: %v", e.Err)
}

func (e *ErrGroupSession) Unwrap() error { return e.Err }
