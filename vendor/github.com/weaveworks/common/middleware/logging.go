package middleware

import (
	"bytes"
	"net/http"
	"net/http/httputil"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/weaveworks/common/user"
)

// Log middleware logs http requests
type Log struct {
	LogRequestHeaders bool // LogRequestHeaders true -> dump http headers at debug log level
}

// logWithRequest information from the request and context as fields.
func logWithRequest(r *http.Request) *log.Entry {
	return log.WithFields(user.LogFields(r.Context()))
}

// Wrap implements Middleware
func (l Log) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		begin := time.Now()
		uri := r.RequestURI // capture the URI before running next, as it may get rewritten
		// Log headers before running 'next' in case other interceptors change the data.
		headers, err := httputil.DumpRequest(r, false)
		if err != nil {
			headers = nil
			logWithRequest(r).Errorf("Could not dump request headers: %v", err)
		}
		var buf bytes.Buffer
		wrapped := newBadResponseLoggingWriter(w, &buf)
		next.ServeHTTP(wrapped, r)
		statusCode := wrapped.statusCode
		if 100 <= statusCode && statusCode < 500 || statusCode == 502 {
			logWithRequest(r).Debugf("%s %s (%d) %s", r.Method, uri, statusCode, time.Since(begin))
			if l.LogRequestHeaders && headers != nil {
				logWithRequest(r).Debugf("Is websocket request: %v\n%s", IsWSHandshakeRequest(r), string(headers))
			}
		} else {
			logWithRequest(r).Warnf("%s %s (%d) %s", r.Method, uri, statusCode, time.Since(begin))
			if headers != nil {
				logWithRequest(r).Warnf("Is websocket request: %v\n%s", IsWSHandshakeRequest(r), string(headers))
			}
			logWithRequest(r).Warnf("Response: %s", buf.Bytes())
		}
	})
}

// Logging middleware logs each HTTP request method, path, response code and
// duration for all HTTP requests.
var Logging = Log{}
