package reader

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/grafana/loki-canary/pkg/comparator"
)

type Reader struct {
	url          url.URL
	header       http.Header
	conn         *websocket.Conn
	w            io.Writer
	cm           *comparator.Comparator
	quit         chan struct{}
	shuttingDown bool
	done         chan struct{}
}

func NewReader(writer io.Writer, comparator *comparator.Comparator, url url.URL, user string, pass string) *Reader {
	h := http.Header{}
	if user != "" {
		h = http.Header{"Authorization": {"Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))}}
	}
	rd := Reader{
		w:            writer,
		url:          url,
		cm:           comparator,
		header:       h,
		quit:         make(chan struct{}),
		done:         make(chan struct{}),
		shuttingDown: false,
	}

	go rd.run()

	go func() {
		select {
		case <-rd.quit:
			if rd.conn != nil {
				_, _ = fmt.Fprintf(rd.w, "shutting down reader\n")
				rd.shuttingDown = true
				_ = rd.conn.Close()
			}
		}
	}()

	return &rd
}

func (r *Reader) Stop() {
	close(r.quit)
	<-r.done
}

func (r *Reader) run() {

	r.closeAndReconnect()

	stream := &Stream{}

	for {
		err := r.conn.ReadJSON(stream)
		if err != nil {
			if r.shuttingDown {
				close(r.done)
				return
			}
			_, _ = fmt.Fprintf(r.w, "error reading websocket: %s\n", err)
			r.closeAndReconnect()
			continue
		}

		for _, entry := range stream.Entries {
			sp := strings.Split(entry.Line, " ")
			if len(sp) != 2 {
				_, _ = fmt.Fprintf(r.w, "received invalid entry: %s\n", entry.Line)
				continue
			}
			ts, err := strconv.ParseInt(sp[0], 10, 64)
			if err != nil {
				_, _ = fmt.Fprintf(r.w, "failed to parse timestamp: %s\n", sp[0])
				continue
			}
			r.cm.EntryReceived(time.Unix(0, ts))
		}
	}
}

func (r *Reader) closeAndReconnect() {
	if r.conn != nil {
		_ = r.conn.Close()
		r.conn = nil
	}
	for r.conn == nil {
		c, _, err := websocket.DefaultDialer.Dial(r.url.String(), r.header)
		if err != nil {
			_, _ = fmt.Fprintf(r.w, "failed to connect to %s with err %s\n", r.url.String(), err)
			<-time.After(5 * time.Second)
			continue
		}
		r.conn = c
	}
}
