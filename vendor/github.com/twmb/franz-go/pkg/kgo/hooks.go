package kgo

import (
	"net"
	"time"
)

////////////////////////////////////////////////////////////////
// NOTE:                                                      //
// NOTE: Make sure new hooks are checked in implementsAnyHook //
// NOTE:                                                      //
////////////////////////////////////////////////////////////////

// Hook is a hook to be called when something happens in kgo.
//
// The base Hook interface is useless, but wherever a hook can occur in kgo,
// the client checks if your hook implements an appropriate interface. If so,
// your hook is called.
//
// This allows you to only hook in to behavior you care about, and it allows
// the client to add more hooks in the future.
//
// All hook interfaces in this package have Hook in the name. Hooks must be
// safe for concurrent use. It is expected that hooks are fast; if a hook needs
// to take time, then copy what you need and ensure the hook is async.
type Hook any

type hooks []Hook

func (hs hooks) each(fn func(Hook)) {
	for _, h := range hs {
		fn(h)
	}
}

// HookNewClient is called in NewClient after a client is initialized. This
// hook can be used to perform final setup work in your hooks.
type HookNewClient interface {
	// OnNewClient is passed the newly initialized client, before any
	// client goroutines are started.
	OnNewClient(*Client)
}

// HookClientClosed is called in Close or CloseAfterRebalance after a client
// has been closed. This hook can be used to perform final cleanup work.
type HookClientClosed interface {
	// OnClientClosed is passed the client that has been closed, after
	// all client-internal close cleanup has happened.
	OnClientClosed(*Client)
}

//////////////////
// BROKER HOOKS //
//////////////////

// HookBrokerConnect is called after a connection to a broker is opened.
type HookBrokerConnect interface {
	// OnBrokerConnect is passed the broker metadata, how long it took to
	// dial, and either the dial's resulting net.Conn or error.
	OnBrokerConnect(meta BrokerMetadata, dialDur time.Duration, conn net.Conn, err error)
}

// HookBrokerDisconnect is called when a connection to a broker is closed.
type HookBrokerDisconnect interface {
	// OnBrokerDisconnect is passed the broker metadata and the connection
	// that is closing.
	OnBrokerDisconnect(meta BrokerMetadata, conn net.Conn)
}

// HookBrokerWrite is called after a write to a broker.
//
// Kerberos SASL does not cause write hooks, since it directly writes to the
// connection.
type HookBrokerWrite interface {
	// OnBrokerWrite is passed the broker metadata, the key for the request
	// that was written, the number of bytes that were written (may not be
	// the whole request if there was an error), how long the request
	// waited before being written (including throttling waiting), how long
	// it took to write the request, and any error.
	//
	// The bytes written does not count any tls overhead.
	OnBrokerWrite(meta BrokerMetadata, key int16, bytesWritten int, writeWait, timeToWrite time.Duration, err error)
}

// HookBrokerRead is called after a read from a broker.
//
// Kerberos SASL does not cause read hooks, since it directly reads from the
// connection.
type HookBrokerRead interface {
	// OnBrokerRead is passed the broker metadata, the key for the response
	// that was read, the number of bytes read (may not be the whole read
	// if there was an error), how long the client waited before reading
	// the response, how long it took to read the response, and any error.
	//
	// The bytes read does not count any tls overhead.
	OnBrokerRead(meta BrokerMetadata, key int16, bytesRead int, readWait, timeToRead time.Duration, err error)
}

// BrokerE2E tracks complete information for a write of a request followed by a
// read of that requests's response.
//
// Note that if this is for a produce request with no acks, there will be no
// read wait / time to read.
type BrokerE2E struct {
	// BytesWritten is the number of bytes written for this request.
	//
	// This may not be the whole request if there was an error while writing.
	BytesWritten int

	// BytesRead is the number of bytes read for this requests's response.
	//
	// This may not be the whole response if there was an error while
	// reading, and this will be zero if there was a write error.
	BytesRead int

	// WriteWait is the time spent waiting from when this request was
	// generated internally in the client to just before the request is
	// written to the connection. This number is not included in the
	// DurationE2E method.
	WriteWait time.Duration
	// TimeToWrite is how long a request took to be written on the wire.
	// This specifically tracks only how long conn.Write takes.
	TimeToWrite time.Duration
	// ReadWait tracks the span of time immediately following conn.Write
	// until conn.Read begins.
	ReadWait time.Duration
	// TimeToRead tracks how long conn.Read takes for this request to be
	// entirely read. This includes the time it takes to allocate a buffer
	// for the response after the initial four size bytes are read.
	TimeToRead time.Duration

	// WriteErr is any error encountered during writing. If a write error is
	// encountered, no read will be attempted.
	WriteErr error
	// ReadErr is any error encountered during reading.
	ReadErr error
}

// DurationE2E returns the e2e time from the start of when a request is written
// to the end of when the response for that request was fully read. If a write
// or read error occurs, this hook is called with all information possible at
// the time (e.g., if a write error occurs, all write info is specified).
//
// Kerberos SASL does not cause this hook, since it directly reads from the
// connection.
func (e *BrokerE2E) DurationE2E() time.Duration {
	return e.TimeToWrite + e.ReadWait + e.TimeToRead
}

// Err returns the first of either the write err or the read err. If this
// return is non-nil, the request/response had an error.
func (e *BrokerE2E) Err() error {
	if e.WriteErr != nil {
		return e.WriteErr
	}
	return e.ReadErr
}

// HookBrokerE2E is called after a write to a broker that errors, or after a
// read to a broker.
//
// This differs from HookBrokerRead and HookBrokerWrite by tracking all E2E
// info for a write and a read, which allows for easier e2e metrics. This hook
// can replace both the read and write hook.
type HookBrokerE2E interface {
	// OnBrokerE2E is passed the broker metadata, the key for the
	// request/response that was written/read, and the e2e info for the
	// request and response.
	OnBrokerE2E(meta BrokerMetadata, key int16, e2e BrokerE2E)
}

// HookBrokerThrottle is called after a response to a request is read
// from a broker, and the response identifies throttling in effect.
type HookBrokerThrottle interface {
	// OnBrokerThrottle is passed the broker metadata, the imposed
	// throttling interval, and whether the throttle was applied before
	// Kafka responded to them request or after.
	//
	// For Kafka < 2.0, the throttle is applied before issuing a response.
	// For Kafka >= 2.0, the throttle is applied after issuing a response.
	//
	// If throttledAfterResponse is false, then Kafka already applied the
	// throttle. If it is true, the client internally will not send another
	// request until the throttle deadline has passed.
	OnBrokerThrottle(meta BrokerMetadata, throttleInterval time.Duration, throttledAfterResponse bool)
}

//////////
// MISC //
//////////

// HookGroupManageError is called after every error that causes the client,
// operating as a group member, to break out of the group managing loop and
// backoff temporarily.
//
// Specifically, any error that would result in OnPartitionsLost being called
// will result in this hook being called.
type HookGroupManageError interface {
	// OnGroupManageError is passed the error that killed a group session.
	// This can be used to detect potentially fatal errors and act on them
	// at runtime to recover (such as group auth errors, or group max size
	// reached).
	OnGroupManageError(error)
}

///////////////////////////////
// PRODUCE & CONSUME BATCHES //
///////////////////////////////

// ProduceBatchMetrics tracks information about successful produces to
// partitions.
type ProduceBatchMetrics struct {
	// NumRecords is the number of records that were produced in this
	// batch.
	NumRecords int

	// UncompressedBytes is the number of bytes the records serialized as
	// before compression.
	//
	// For record batches (Kafka v0.11.0+), this is the size of the records
	// in a batch, and does not include record batch overhead.
	//
	// For message sets, this size includes message set overhead.
	UncompressedBytes int

	// CompressedBytes is the number of bytes actually written for this
	// batch, after compression. If compression is not used, this will be
	// equal to UncompresedBytes.
	//
	// For record batches, this is the size of the compressed records, and
	// does not include record batch overhead.
	//
	// For message sets, this is the size of the compressed message set.
	CompressedBytes int

	// CompressionType signifies which algorithm the batch was compressed
	// with.
	//
	// 0 is no compression, 1 is gzip, 2 is snappy, 3 is lz4, and 4 is
	// zstd.
	CompressionType uint8
}

// HookProduceBatchWritten is called whenever a batch is known to be
// successfully produced.
type HookProduceBatchWritten interface {
	// OnProduceBatchWritten is called per successful batch written to a
	// topic partition
	OnProduceBatchWritten(meta BrokerMetadata, topic string, partition int32, metrics ProduceBatchMetrics)
}

// FetchBatchMetrics tracks information about fetches of batches.
type FetchBatchMetrics struct {
	// NumRecords is the number of records that were fetched in this batch.
	//
	// Note that this number includes transaction markers, which are not
	// actually returned to the user.
	//
	// If the batch has an encoding error, this will be 0.
	NumRecords int

	// UncompressedBytes is the number of bytes the records deserialized
	// into after decompresion.
	//
	// For record batches (Kafka v0.11.0+), this is the size of the records
	// in a batch, and does not include record batch overhead.
	//
	// For message sets, this size includes message set overhead.
	//
	// Note that this number may be higher than the corresponding number
	// when producing, because as an "optimization", Kafka can return
	// partial batches when fetching.
	UncompressedBytes int

	// CompressedBytes is the number of bytes actually read for this batch,
	// before decompression. If the batch was not compressed, this will be
	// equal to UncompressedBytes.
	//
	// For record batches, this is the size of the compressed records, and
	// does not include record batch overhead.
	//
	// For message sets, this is the size of the compressed message set.
	CompressedBytes int

	// CompressionType signifies which algorithm the batch was compressed
	// with.
	//
	// 0 is no compression, 1 is gzip, 2 is snappy, 3 is lz4, and 4 is
	// zstd.
	CompressionType uint8
}

// HookFetchBatchRead is called whenever a batch if read within the client.
//
// Note that this hook is called when processing, but a batch may be internally
// discarded after processing in some uncommon specific circumstances.
//
// If the client reads v0 or v1 message sets, and they are not compressed, then
// this hook will be called per record.
type HookFetchBatchRead interface {
	// OnFetchBatchRead is called per batch read from a topic partition.
	OnFetchBatchRead(meta BrokerMetadata, topic string, partition int32, metrics FetchBatchMetrics)
}

///////////////////////////////
// PRODUCE & CONSUME RECORDS //
///////////////////////////////

// HookProduceRecordBuffered is called when a record is buffered internally in
// the client from a call to Produce.
//
// This hook can be used to write metrics that gather the number of records or
// bytes buffered, or the hook can be used to write interceptors that modify a
// record's key / value / headers before being produced. If you just want a
// metric for the number of records buffered, use the client's
// BufferedProduceRecords method, as it is faster.
//
// Note that this hook may slow down high-volume producing a bit.
type HookProduceRecordBuffered interface {
	// OnProduceRecordBuffered is passed a record that is buffered.
	//
	// This hook is called immediately after Produce is called, after the
	// function potentially sets the default topic.
	OnProduceRecordBuffered(*Record)
}

// HookProduceRecordPartitioned is called when a record is partitioned and
// internally ready to be flushed.
//
// This hook can be used to create metrics of buffered records per partition,
// and then you can correlate that to partition leaders and determine which
// brokers are having problems.
//
// Note that this hook will slow down high-volume producing and it is
// recommended to only use this temporarily or if you are ok with the
// performance hit.
type HookProduceRecordPartitioned interface {
	// OnProduceRecordPartitioned is passed a record that has been
	// partitioned and the current broker leader for the partition
	// (note that the leader may change if the partition is moved).
	//
	// This hook is called once a record is queued to be flushed. The
	// record's Partition and Timestamp fields are safe to read.
	OnProduceRecordPartitioned(*Record, int32)
}

// HookProduceRecordUnbuffered is called just before a record's promise is
// finished; this is effectively a mirror of a record promise.
//
// As an example, if using HookProduceRecordBuffered for a gauge of how many
// record bytes are buffered, this hook can be used to decrement the gauge.
//
// Note that this hook will slow down high-volume producing a bit.
type HookProduceRecordUnbuffered interface {
	// OnProduceRecordUnbuffered is passed a record that is just about to
	// have its produce promise called, as well as the error that the
	// promise will be called with.
	OnProduceRecordUnbuffered(*Record, error)
}

// HookFetchRecordBuffered is called when a record is internally buffered after
// fetching, ready to be polled.
//
// This hook can be used to write gauge metrics regarding the number of records
// or bytes buffered, or to write interceptors that modify a record before
// being returned from polling. If you just want a metric for the number of
// records buffered, use the client's BufferedFetchRecords method, as it is
// faster.
//
// Note that this hook will slow down high-volume consuming a bit.
type HookFetchRecordBuffered interface {
	// OnFetchRecordBuffered is passed a record that is now buffered, ready
	// to be polled.
	OnFetchRecordBuffered(*Record)
}

// HookFetchRecordUnbuffered is called when a fetched record is unbuffered.
//
// A record can be internally discarded after being in some scenarios without
// being polled, such as when the internal assignment changes.
//
// As an example, if using HookFetchRecordBuffered for a gauge of how many
// record bytes are buffered ready to be polled, this hook can be used to
// decrement the gauge.
//
// Note that this hook may slow down high-volume consuming a bit.
type HookFetchRecordUnbuffered interface {
	// OnFetchRecordUnbuffered is passwed a record that is being
	// "unbuffered" within the client, and whether the record is being
	// returned from polling.
	OnFetchRecordUnbuffered(r *Record, polled bool)
}

/////////////
// HELPERS //
/////////////

// implementsAnyHook will check the incoming Hook for any Hook implementation
func implementsAnyHook(h Hook) bool {
	switch h.(type) {
	case HookNewClient,
		HookClientClosed,
		HookBrokerConnect,
		HookBrokerDisconnect,
		HookBrokerWrite,
		HookBrokerRead,
		HookBrokerE2E,
		HookBrokerThrottle,
		HookGroupManageError,
		HookProduceBatchWritten,
		HookFetchBatchRead,
		HookProduceRecordBuffered,
		HookProduceRecordPartitioned,
		HookProduceRecordUnbuffered,
		HookFetchRecordBuffered,
		HookFetchRecordUnbuffered:
		return true
	}
	return false
}
