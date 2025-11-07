package query

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
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

// TailQuery connects to the Loki websocket endpoint and tails logs
func (q *Query) TailQuery(delayFor time.Duration, c client.Client, out output.LogOutput) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	conn, err := c.LiveTailQueryConn(q.QueryString, delayFor, q.Limit, q.Start, q.Quiet)
	if err != nil {
		return err
	}

	go func() {
		stopChan := make(chan os.Signal, 1)
		signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)
		<-stopChan
		cancel()
		if err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")); err != nil {
			log.Println("error closing websocket:", err)
		}
		_ = conn.Close()
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
		err := unmarshal.ReadTailResponseJSON(tailResponse, conn)
		if err != nil {
			// Check if the websocket connection closed unexpectedly. If so, retry.
			// The connection might close unexpectedly if the querier handling the tail request
			// in Loki stops running. The following error would be printed:
			// "websocket: close 1006 (abnormal closure): unexpected EOF"
			if websocket.IsCloseError(err, websocket.CloseAbnormalClosure) {
				if ctx.Err() != nil {
					return nil
				}
				log.Printf("remote websocket connection closed unexpectedly (%+v). Connecting again.", err)

				// Close previous connection. If it fails to close the connection it should be fine as it is already broken.
				if err = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")); err != nil {
					log.Printf("error closing websocket: %+v", err)
				}

				// Try to re-establish the connection up to 5 times.
				bo := backoff.New(ctx, backoff.Config{
					MinBackoff: 1 * time.Second,
					MaxBackoff: 10 * time.Second,
					MaxRetries: 5,
				})

				for bo.Ongoing() {
					conn, err = c.LiveTailQueryConn(q.QueryString, delayFor, q.Limit, lastReceivedTimestamp, q.Quiet)
					if err == nil {
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
					return nil
				}

				continue
			}

			log.Println("error reading stream:", err)
			return nil
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
