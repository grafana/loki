package tailproxy

import (
	"github.com/cortexproject/cortex/pkg/querier/frontend"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gorilla/websocket"
	"github.com/grafana/loki/pkg/logcli/client"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/prometheus/common/config"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

type WsTailProxy struct {
	downstreamURL string
	log           log.Logger
}

func New(cfg frontend.Config, log log.Logger) *WsTailProxy {
	return &WsTailProxy{
		downstreamURL: cfg.DownstreamURL,
		log:           log,
	}
}

type closer struct {
	sync.Mutex
	closed bool
}

func (c *closer) CloseFunc(f func()) {
	c.Lock()
	defer c.Unlock()
	if c.closed {
		return
	}
	f()
	c.closed = true
}

func (c *closer) Close() {
	c.Lock()
	defer c.Unlock()
	if c.closed {
		return
	}
	c.closed = true
}

func (c *closer) isClosed() bool {
	c.Lock()
	defer c.Unlock()
	return c.closed
}

func (tp WsTailProxy) Handle(w http.ResponseWriter, r *http.Request) {
	logger := util.WithContext(r.Context(), tp.log)

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		level.Error(logger).Log("msg", "Error in upgrading websocket", "err", err)
		return
	}
	conCloser := &closer{}

	defer func() {
		if err := conn.Close(); err != nil {
			level.Error(logger).Log("msg", "Error closing websocket", "err", err)
		}
	}()

	auth := strings.Split(strings.ReplaceAll(r.Header.Get("Authorization"), "Basic ", ""), ":")
	user := auth[0]
	var password string
	if len(auth) > 1 {
		password = auth[1]
	} else {
		password = ""
	}

	tailClient := &client.Client{
		TLSConfig: config.TLSConfig{},
		Address:   tp.downstreamURL,
		OrgID:     r.Header.Get("X-Scope-OrgID"),
		Username:  user,
		Password:  password,
	}

	q := r.URL.Query()
	from, err := strconv.Atoi(q.Get("start"))
	if err != nil {
		level.Error(logger).Log("msg", "Missing start", "err", err)
		return
	}
	delayFor, err := strconv.Atoi(q.Get("delayFor"))
	if err != nil {
		delayFor = 0
	}
	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil {
		limit = 30
	}

	clientConn, err := tailClient.LiveTailQueryConn(q.Get("query"), delayFor, limit, int64(from), false)
	if err != nil {
		level.Error(logger).Log("msg", "TailClient connection failed", "err", err)
		return
	}
	clientConCloser := &closer{}
	defer func() {
		clientConCloser.CloseFunc(func() {
			if err := clientConn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")); err != nil {
				level.Error(logger).Log("msg", "Error closing websocket:", "err", err)
			}
		})
	}()

	tailReponse := new(loghttp.TailResponse)
	wg := &sync.WaitGroup{}
	wg.Add(1)

	go func() {
		defer wg.Done()
		for {
			err = clientConn.ReadJSON(tailReponse)
			if err != nil {
				if closeErr, ok := err.(*websocket.CloseError); ok {
					if closeErr.Code == websocket.CloseNormalClosure {
						clientConCloser.Close()
						break
					}
					level.Error(logger).Log("msg", "Error from tailClient", "err", err)
					clientConCloser.Close()
					break
				} else {
					level.Error(logger).Log("msg", "Unexpected error from tailClient", "err", err)
					clientConCloser.Close()
					break
				}
			} else {
				if conCloser.isClosed() {
					break
				}
				err = conn.WriteJSON(tailReponse)
				if err != nil {
					level.Error(logger).Log("msg", "ws write err", "err", err)
					conCloser.CloseFunc(func() {
						if err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error())); err != nil {
							level.Error(logger).Log("msg", "Error writing close message to websocket", "err", err)
						}
					})
					break
				}
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			err := conn.ReadJSON(tailReponse)
			if err != nil {
				if closeErr, ok := err.(*websocket.CloseError); ok {
					if closeErr.Code == websocket.CloseNormalClosure {
						conCloser.Close()
						break
					}
					level.Error(logger).Log("msg", "Error from client", "err", err)
					conCloser.Close()
					break
				} else {
					level.Error(logger).Log("msg", "Unexpected error from client", "err", err)
					conCloser.Close()
					break
				}
			} else {
				if clientConCloser.isClosed() {
					break
				}
				err = clientConn.WriteJSON(tailReponse)
				if err != nil {
					level.Error(logger).Log("msg", "tailClient write err", "err", err)
					clientConCloser.CloseFunc(func() {
						if err := clientConn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error())); err != nil {
							level.Error(logger).Log("msg", "Error writing close message to websocket", "err", err)
						}
					})
					break
				}
			}
		}
	}()

	wg.Wait()
	conCloser.CloseFunc(func() {
		if err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")); err != nil {
			level.Error(logger).Log("msg", "Error closing websocket:", "err", err)
		}
	})
}
