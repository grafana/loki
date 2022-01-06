package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/common/model"
)

const (
	timeout    = 5 * time.Second
	minBackoff = 100 * time.Millisecond
	maxBackoff = 30 * time.Second
	maxRetries = 10

	reservedLabelTenantID = "__tenant_id__"

	userAgent = "lambda-promtail"
)

type entry struct {
	labels model.LabelSet
	entry  logproto.Entry
}

type batch struct {
	streams map[string]*logproto.Stream
}

func newBatch(entries ...entry) *batch {
	b := &batch{
		streams: map[string]*logproto.Stream{},
	}

	for _, entry := range entries {
		b.add(entry)
	}

	return b
}

func (b *batch) add(e entry) {
	labels := labelsMapToString(e.labels, reservedLabelTenantID)
	if stream, ok := b.streams[labels]; ok {
		stream.Entries = append(stream.Entries, e.entry)
		return
	}

	b.streams[labels] = &logproto.Stream{
		Labels:  labels,
		Entries: []logproto.Entry{e.entry},
	}
}

func labelsMapToString(ls model.LabelSet, without ...model.LabelName) string {
	lstrs := make([]string, 0, len(ls))
Outer:
	for l, v := range ls {
		for _, w := range without {
			if l == w {
				continue Outer
			}
		}
		lstrs = append(lstrs, fmt.Sprintf("%s=%q", l, v))
	}

	sort.Strings(lstrs)
	return fmt.Sprintf("{%s}", strings.Join(lstrs, ", "))
}

func (b *batch) encode() ([]byte, int, error) {
	req, entriesCount := b.createPushRequest()
	buf, err := proto.Marshal(req)
	if err != nil {
		return nil, 0, err
	}

	buf = snappy.Encode(nil, buf)
	return buf, entriesCount, nil
}

func (b *batch) createPushRequest() (*logproto.PushRequest, int) {
	req := logproto.PushRequest{
		Streams: make([]logproto.Stream, 0, len(b.streams)),
	}

	entriesCount := 0
	for _, stream := range b.streams {
		req.Streams = append(req.Streams, *stream)
		entriesCount += len(stream.Entries)
	}
	return &req, entriesCount
}

func sendToPromtail(ctx context.Context, b *batch) error {
	buf, _, err := b.encode()
	if err != nil {
		return err
	}

	backoff := util.NewBackoff(ctx, util.BackoffConfig{minBackoff, maxBackoff, maxRetries})
	var status int
	for {
		// send uses `timeout` internally, so `context.Background` is good enough.
		status, err = send(context.Background(), buf)

		// Only retry 429s, 500s and connection-level errors.
		if status > 0 && status != 429 && status/100 != 5 {
			break
		}

		fmt.Printf("error sending batch, will retry, status: %d error: %s\n", status, err)
		backoff.Wait()

		// Make sure it sends at least once before checking for retry.
		if !backoff.Ongoing() {
			break
		}
	}

	if err != nil {
		fmt.Printf("Failed to send logs! %s\n", err)
		return err
	}

	return nil
}

func send(ctx context.Context, buf []byte) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequest("POST", writeAddress.String(), bytes.NewReader(buf))
	if err != nil {
		return -1, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", userAgent)

	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		return -1, err
	}

	if resp.StatusCode/100 != 2 {
		scanner := bufio.NewScanner(io.LimitReader(resp.Body, maxErrMsgLen))
		line := ""
		if scanner.Scan() {
			line = scanner.Text()
		}
		err = fmt.Errorf("server returned HTTP status %s (%d): %s", resp.Status, resp.StatusCode, line)
	}

	return resp.StatusCode, err
}
