package query

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/gorilla/websocket"
	"github.com/grafana/dskit/backoff"

	"github.com/grafana/loki/v3/pkg/logcli/client"
	"github.com/grafana/loki/v3/pkg/logcli/output"
	"github.com/grafana/loki/v3/pkg/logcli/util"
	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/util/unmarshal"
)

type contextTailClient interface {
	LiveTailQueryConnContext(
		ctx context.Context,
		queryStr string,
		delayFor time.Duration,
		limit int,
		start time.Time,
		quiet bool,
	) (*websocket.Conn, error)
}

type tailConnectionResult struct {
	conn *websocket.Conn
	err  error
}

func liveTailQueryConn(
	ctx context.Context,
	c client.Client,
	queryString string,
	delayFor time.Duration,
	limit int,
	start time.Time,
	quiet bool,
) (*websocket.Conn, error) {
	if contextClient, ok := c.(contextTailClient); ok {
		return contextClient.LiveTailQueryConnContext(ctx, queryString, delayFor, limit, start, quiet)
	}

	resultChan := make(chan tailConnectionResult)
	go func() {
		conn, err := c.LiveTailQueryConn(queryString, delayFor, limit, start, quiet)
		select {
		case resultChan <- tailConnectionResult{conn: conn, err: err}:
		case <-ctx.Done():
			if conn != nil {
				_ = conn.Close()
			}
		}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-resultChan:
		if ctx.Err() != nil {
			if result.conn != nil {
				_ = result.conn.Close()
			}
			return nil, ctx.Err()
		}
		return result.conn, result.err
	}
}

// TailQuery connects to the Loki websocket endpoint and tails logs
func (q *Query) TailQuery(delayFor time.Duration, c client.Client, out output.LogOutput) error {
	conn, err := c.LiveTailQueryConn(q.QueryString, delayFor, q.Limit, q.Start, q.Quiet)
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
	c client.Client,
	out output.LogOutput,
	initialConn *websocket.Conn,
	stopChan <-chan os.Signal,
) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var conn atomic.Pointer[websocket.Conn]
	conn.Store(initialConn)

	done := make(chan struct{})
	var signalWG sync.WaitGroup
	signalWG.Add(1)
	go func() {
		defer signalWG.Done()

		select {
		case <-stopChan:
			cancel()
			currentConn := conn.Load()
			if err := currentConn.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
				time.Now().Add(time.Second),
			); err != nil {
				log.Println("error closing websocket:", err)
			}
			_ = currentConn.Close()
		case <-done:
		}
	}()
	defer func() {
		close(done)
		signalWG.Wait()
		_ = conn.Load().Close()
	}()

	if len(q.IgnoreLabelsKey) > 0 && !q.Quiet {
		log.Println("ignoring labels key:", color.RedString(strings.Join(q.IgnoreLabelsKey, ",")))
	}

	if len(q.ShowLabelsKey) > 0 && !q.Quiet {
		log.Println("print only labels key:", color.RedString(strings.Join(q.ShowLabelsKey, ",")))
	}

	lastReceivedTimestamp := q.Start

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		tailResponse := new(loghttp.TailResponse)
		currentConn := conn.Load()
		err := unmarshal.ReadTailResponseJSON(tailResponse, currentConn)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}

			// Check if the websocket connection closed unexpectedly. If so, retry.
			// The connection might close unexpectedly if the querier handling the tail request
			// in Loki stops running. The following error would be printed:
			// "websocket: close 1006 (abnormal closure): unexpected EOF"
			if websocket.IsCloseError(err, websocket.CloseAbnormalClosure) {
				log.Printf("remote websocket connection closed unexpectedly (%+v). Connecting again.", err)

				// Close previous connection. If it fails to close the connection it should be fine as it is already broken.
				if err = currentConn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")); err != nil {
					log.Printf("error closing websocket: %+v", err)
				}
				_ = currentConn.Close()

				// Try to re-establish the connection up to 5 times.
				bo := backoff.New(ctx, backoff.Config{
					MinBackoff: 1 * time.Second,
					MaxBackoff: 10 * time.Second,
					MaxRetries: 5,
				})

				for bo.Ongoing() {
					var nextConn *websocket.Conn
					nextConn, err = liveTailQueryConn(ctx, c, q.QueryString, delayFor, q.Limit, lastReceivedTimestamp, q.Quiet)
					if err == nil {
						if ctx.Err() != nil {
							_ = nextConn.Close()
							return nil
						}
						conn.Store(nextConn)
						break
					}

					if ctx.Err() != nil {
						return nil
					}

					log.Println("error recreating tailing connection after unexpected close, will retry:", err)
					bo.Wait()
				}

				if err = bo.Err(); err != nil {
					if ctx.Err() != nil {
						return nil
					}
					log.Println("error recreating tailing connection:", err)
					return fmt.Errorf("recreating tailing connection: %w", err)
				}

				continue
			}

			log.Println("error reading stream:", err)
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
						ls = matchLabels(false, ls, q.ShowLabelsKey)
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
			log.Println("server dropped following entries due to slow client")
			for _, d := range tailResponse.DroppedStreams {
				log.Println(d.Timestamp, d.Labels)
			}
		}
	}
}

func matchLabels(on bool, l loghttp.LabelSet, names []string) loghttp.LabelSet {
	return util.MatchLabels(on, l, names)
}
