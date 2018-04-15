package billing

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/fluent/fluent-logger-golang/fluent"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/weaveworks/common/instrument"
)

var (
	// requestCollector is the duration of billing client requests
	requestCollector = instrument.NewHistogramCollector(prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "billing_client",
		Name:      "request_duration_seconds",
		Help:      "Time in seconds spent emitting billing info.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method", "status_code"}))

	// EventsCounter is the count of billing events
	EventsCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "billing_client",
		Name:      "events_total",
		Help:      "Number and type of billing events",
	}, []string{"status", "amount_type"})
	// AmountsCounter is the total of the billing amounts
	AmountsCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "billing_client",
		Name:      "amounts_total",
		Help:      "Number and type of billing amounts",
	}, []string{"status", "amount_type"})
)

// MustRegisterMetrics is a convenience function for registering all the metrics from this package
func MustRegisterMetrics() {
	requestCollector.Register()
	prometheus.MustRegister(EventsCounter)
	prometheus.MustRegister(AmountsCounter)
}

// Client is a billing client for sending usage information to the billing system.
type Client struct {
	stop   chan struct{}
	wg     sync.WaitGroup
	events chan Event
	logger *fluent.Fluent
	Config
}

// NewClient creates a new billing client.
func NewClient(cfg Config) (*Client, error) {
	host, port, err := net.SplitHostPort(cfg.IngesterHostPort)
	if err != nil {
		return nil, err
	}
	intPort, err := strconv.Atoi(port)
	if err != nil {
		return nil, err
	}
	logger, err := fluent.New(fluent.Config{
		FluentPort:    intPort,
		FluentHost:    host,
		AsyncConnect:  true,
		MaxRetry:      -1,
		MarshalAsJSON: true,
	})
	if err != nil {
		return nil, err
	}

	c := &Client{
		stop:   make(chan struct{}),
		events: make(chan Event, cfg.MaxBufferedEvents),
		logger: logger,
		Config: cfg,
	}
	c.wg.Add(1)
	go c.loop()
	return c, nil
}

// AddAmounts writes unit increments into the billing system. If the call does
// not complete (due to a crash, etc), then data may or may not have been
// written successfully.
//
// Requests with the same `uniqueKey` can be retried indefinitely until they
// succeed, and the results will be deduped.
//
// `uniqueKey` must be set, and not blank. If in doubt, generate a uuid and set
// that as the uniqueKey. Consider that hashing the raw input data may not be
// good enough since identical data may be sent from the client multiple times.
//
// `internalInstanceID`, is *not* the external instance ID (e.g.
// "fluffy-bunny-47"), it is the numeric internal instance ID (e.g. "1234").
//
// `timestamp` is used to determine which time bucket the usage occurred in, it
// is included so that the result is independent of how long processing takes.
// Note, in the event of buffering this timestamp may *not* agree with when the
// charge will be billed to the customer.
//
// `amounts` is a map with all the various amounts we wish to charge the user
// for.
//
// `metadata` is a general dumping ground for other metadata you may wish to
// include for auditability. In general, be careful about the size of data put
// here. Prefer including a lookup address over whole data. For example,
// include a report id or s3 address instead of the information in the report.
func (c *Client) AddAmounts(uniqueKey, internalInstanceID string, timestamp time.Time, amounts Amounts, metadata map[string]string) error {
	return instrument.CollectedRequest(context.Background(), "Billing.AddAmounts", requestCollector, nil, func(_ context.Context) error {
		if uniqueKey == "" {
			return fmt.Errorf("billing: units uniqueKey cannot be blank")
		}

		e := Event{
			UniqueKey:          uniqueKey,
			InternalInstanceID: internalInstanceID,
			OccurredAt:         timestamp,
			Amounts:            amounts,
			Metadata:           metadata,
		}

		select {
		case <-c.stop:
			trackEvent("stopping", e)
			return fmt.Errorf("billing: stopping, discarding event: %v", e)
		default:
		}

		select {
		case c.events <- e: // Put event in the channel unless it is full
			return nil
		default:
			// full
		}
		trackEvent("buffer_full", e)
		return fmt.Errorf("billing: reached billing event buffer limit (%d), discarding event: %v", c.MaxBufferedEvents, e)
	})
}

func (c *Client) loop() {
	defer c.wg.Done()
	for done := false; !done; {
		select {
		case event := <-c.events:
			c.post(event)
		case <-c.stop:
			done = true
		}
	}

	// flush remaining events
	for done := false; !done; {
		select {
		case event := <-c.events:
			c.post(event)
		default:
			done = true
		}
	}
}

func (c *Client) post(e Event) error {
	for {
		var err error
		for _, r := range e.toRecords() {
			if err = c.logger.Post("billing", r); err != nil {
				break
			}
		}
		if err == nil {
			trackEvent("success", e)
			return nil
		}
		select {
		case <-c.stop:
			// We're quitting, no retries.
			trackEvent("stopping", e)
			log.Errorf("billing: failed to log event: %v: %v, stopping", e, err)
			return err
		default:
			trackEvent("retrying", e)
			log.Errorf("billing: failed to log event: %v: %v, retrying in %v", e, err, c.RetryDelay)
			time.Sleep(c.RetryDelay)
		}
	}
}

func trackEvent(status string, e Event) {
	for t, v := range e.Amounts {
		EventsCounter.WithLabelValues(status, string(t)).Inc()
		AmountsCounter.WithLabelValues(status, string(t)).Add(float64(v))
	}
}

// Close shuts down the client and attempts to flush remaining events.
func (c *Client) Close() error {
	close(c.stop)
	c.wg.Wait()
	return c.logger.Close()
}
