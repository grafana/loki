package middleware

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/weaveworks/common/logging"
	"github.com/weaveworks/common/tracing"
	"github.com/weaveworks/common/user"
)

// Log middleware logs http requests
type Log struct {
	Log                   logging.Interface
	LogRequestHeaders     bool // LogRequestHeaders true -> dump http headers at debug log level
	LogRequestAtInfoLevel bool // LogRequestAtInfoLevel true -> log requests at info log level
	SourceIPs             *SourceIPExtractor
}

// logWithRequest information from the request and context as fields.
func (l Log) logWithRequest(r *http.Request) logging.Interface {
	localLog := l.Log
	traceID, ok := tracing.ExtractTraceID(r.Context())
	if ok {
		localLog = localLog.WithField("traceID", traceID)
	}

	if l.SourceIPs != nil {
		ips := l.SourceIPs.Get(r)
		if ips != "" {
			localLog = localLog.WithField("sourceIPs", ips)
		}
	}

	return user.LogWith(r.Context(), localLog)
}

// Wrap implements Middleware
func (l Log) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		begin := time.Now()
		uri := r.RequestURI // capture the URI before running next, as it may get rewritten
		requestLog := l.logWithRequest(r)
		// Log headers before running 'next' in case other interceptors change the data.
		headers, err := dumpRequest(r)
		if err != nil {
			headers = nil
			requestLog.Errorf("Could not dump request headers: %v", err)
		}
		var buf bytes.Buffer
		wrapped := newBadResponseLoggingWriter(w, &buf)
		next.ServeHTTP(wrapped, r)

		statusCode, writeErr := wrapped.getStatusCode(), wrapped.getWriteError()

		if writeErr != nil {
			if errors.Is(writeErr, context.Canceled) {
				if l.LogRequestAtInfoLevel {
					requestLog.Infof("%s %s %s, request cancelled: %s ws: %v; %s", r.Method, uri, time.Since(begin), writeErr, IsWSHandshakeRequest(r), headers)
				} else {
					requestLog.Debugf("%s %s %s, request cancelled: %s ws: %v; %s", r.Method, uri, time.Since(begin), writeErr, IsWSHandshakeRequest(r), headers)
				}
			} else {
				requestLog.Warnf("%s %s %s, error: %s ws: %v; %s", r.Method, uri, time.Since(begin), writeErr, IsWSHandshakeRequest(r), headers)
			}

			return
		}
		if 100 <= statusCode && statusCode < 500 || statusCode == http.StatusBadGateway || statusCode == http.StatusServiceUnavailable {
			if l.LogRequestAtInfoLevel {
				requestLog.Infof("%s %s (%d) %s", r.Method, uri, statusCode, time.Since(begin))
			} else {
				requestLog.Debugf("%s %s (%d) %s", r.Method, uri, statusCode, time.Since(begin))
			}
			if l.LogRequestHeaders && headers != nil {
				if l.LogRequestAtInfoLevel {
					requestLog.Infof("ws: %v; %s", IsWSHandshakeRequest(r), string(headers))
				} else {
					requestLog.Debugf("ws: %v; %s", IsWSHandshakeRequest(r), string(headers))
				}
			}
		} else {
			requestLog.Warnf("%s %s (%d) %s Response: %q ws: %v; %s",
				r.Method, uri, statusCode, time.Since(begin), buf.Bytes(), IsWSHandshakeRequest(r), headers)
		}
	})
}

// Logging middleware logs each HTTP request method, path, response code and
// duration for all HTTP requests.
var Logging = Log{
	Log: logging.Global(),
}

func dumpRequest(req *http.Request) ([]byte, error) {
	var b bytes.Buffer

	// Exclude some headers for security, or just that we don't need them when debugging
	err := req.Header.WriteSubset(&b, map[string]bool{
		"Cookie":        true,
		"X-Csrf-Token":  true,
		"Authorization": true,
	})
	if err != nil {
		return nil, err
	}

	ret := bytes.Replace(b.Bytes(), []byte("\r\n"), []byte("; "), -1)
	return ret, nil
}
