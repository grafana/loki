package query

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/grafana/dskit/backoff"
	log "github.com/sirupsen/logrus"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/lokitool/client"
	"github.com/grafana/loki/v3/pkg/lokitool/output"
	"github.com/grafana/loki/v3/pkg/lokitool/util"
	"github.com/grafana/loki/v3/pkg/util/unmarshal"
)

// TailQuery connects to the Loki websocket endpoint and tails logs
func (q *Query) TailQuery(delayFor time.Duration, c client.TailClient, out output.LogOutput) error {
	conn, err := c.LiveTailQueryConnContext(context.Background(), q.QueryString, delayFor, q.Limit, q.Start)
	if err != nil {
		return err
	}

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stopChan)

	return q.tailQuery(delayFor, c, out, conn, stopChan)
}

func (q *Query) tailQuery(
	delayFor time.Duration,
	c client.TailClient,
	out output.LogOutput,
	initialConn *websocket.Conn,
	stopChan <-chan os.Signal,
) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn := initialConn
	var connMu sync.Mutex
	var stopping atomic.Bool

	done := make(chan struct{})
	var signalWG sync.WaitGroup
	signalWG.Add(1)
	go func() {
		defer signalWG.Done()

		select {
		case <-stopChan:
			// Mark shutdown before sending the close frame: the peer may
			// respond before we cancel the context. Serialize this with
			// installing a reconnected socket so shutdown cannot miss it.
			connMu.Lock()
			stopping.Store(true)
			currentConn := conn
			connMu.Unlock()
			if err := currentConn.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
				time.Now().Add(time.Second),
			); err != nil {
				log.WithError(err).Warn("Error closing websocket")
			}
			cancel()
			_ = currentConn.Close()
		case <-done:
		}
	}()
	defer func() {
		close(done)
		signalWG.Wait()
		_ = conn.Close()
	}()

	if len(q.IgnoreLabelsKey) > 0 {
		log.Debugf("Ignoring labels key: %s", strings.Join(q.IgnoreLabelsKey, ","))
	}

	if len(q.ShowLabelsKey) > 0 {
		log.Debugf("Print only labels key: %s", strings.Join(q.ShowLabelsKey, ","))
	}

	lastReceivedTimestamp := q.Start

	for {
		if stopping.Load() {
			return nil
		}

		tailResponse := new(loghttp.TailResponse)
		currentConn := conn
		err := unmarshal.ReadTailResponseJSON(tailResponse, currentConn)
		if err != nil {
			if stopping.Load() {
				return nil
			}

			// Check if the websocket connection closed unexpectedly. If so, retry.
			// The connection might close unexpectedly if the querier handling the tail request
			// in Loki stops running. The following error would be printed:
			// "websocket: close 1006 (abnormal closure): unexpected EOF"
			if websocket.IsCloseError(err, websocket.CloseAbnormalClosure) {
				log.WithError(err).Warn("Remote websocket connection closed unexpectedly. Connecting again.")

				// Close previous connection. If it fails to close the connection it should be fine as it is already broken.
				if err = currentConn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")); err != nil {
					log.WithError(err).Warn("Error closing websocket")
				}
				_ = currentConn.Close()

				// Try to re-establish the connection up to 5 times.
				bo := backoff.New(ctx, backoff.Config{
					MinBackoff: 1 * time.Second,
					MaxBackoff: 10 * time.Second,
					MaxRetries: 5,
				})

				for bo.Ongoing() && !stopping.Load() {
					var nextConn *websocket.Conn
					nextConn, err = c.LiveTailQueryConnContext(ctx, q.QueryString, delayFor, q.Limit, lastReceivedTimestamp)
					if err == nil {
						connMu.Lock()
						if stopping.Load() {
							connMu.Unlock()
							_ = nextConn.Close()
							return nil
						}
						conn = nextConn
						connMu.Unlock()
						break
					}

					if stopping.Load() {
						return nil
					}

					log.WithError(err).Warn("Error recreating tailing connection after unexpected close, will retry")
					bo.Wait()
				}

				if err = bo.Err(); err != nil {
					if stopping.Load() {
						return nil
					}
					return fmt.Errorf("recreating tailing connection: %w", err)
				}

				continue
			}

			return fmt.Errorf("reading tail stream: %w", err)
		}

		labels := loghttp.LabelSet{}
		for _, stream := range tailResponse.Streams {
			if !q.NoLabels {
				if len(q.IgnoreLabelsKey) > 0 || len(q.ShowLabelsKey) > 0 {

					ls := stream.Labels

					if len(q.ShowLabelsKey) > 0 {
						ls = matchLabels(true, ls, q.ShowLabelsKey)
					}

					if len(q.IgnoreLabelsKey) > 0 {
						ls = matchLabels(false, ls, q.IgnoreLabelsKey)
					}

					labels = ls

				} else {
					labels = stream.Labels
				}
			}

			for _, entry := range stream.Entries {
				if err := out.FormatAndPrintln(entry.Timestamp, labels, 0, entry.Line); err != nil {
					return err
				}
				lastReceivedTimestamp = entry.Timestamp
			}

		}
		if len(tailResponse.DroppedStreams) != 0 {
			log.Warn("Server dropped following entries due to slow client")
			for _, d := range tailResponse.DroppedStreams {
				log.WithFields(log.Fields{"timestamp": d.Timestamp, "labels": d.Labels}).Warn("Dropped entry")
			}
		}
	}
}

func matchLabels(on bool, l loghttp.LabelSet, names []string) loghttp.LabelSet {
	return util.MatchLabels(on, l, names)
}
