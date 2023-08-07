// Provenance-includes-location: https://github.com/weaveworks/common/blob/main/middleware/logging.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: Weaveworks Ltd.

package middleware

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/grafana/dskit/log"
	"github.com/grafana/dskit/tracing"
	"github.com/grafana/dskit/user"
)

// Log middleware logs http requests
type Log struct {
	Log                      log.Interface
	DisableRequestSuccessLog bool
	LogRequestHeaders        bool // LogRequestHeaders true -> dump http headers at debug log level
	LogRequestAtInfoLevel    bool // LogRequestAtInfoLevel true -> log requests at info log level
	SourceIPs                *SourceIPExtractor
	HTTPHeadersToExclude     map[string]bool
}

var defaultExcludedHeaders = map[string]bool{
	"Cookie":        true,
	"X-Csrf-Token":  true,
	"Authorization": true,
}

func NewLogMiddleware(log log.Interface, logRequestHeaders bool, logRequestAtInfoLevel bool, sourceIPs *SourceIPExtractor, headersList []string) Log {
	httpHeadersToExclude := map[string]bool{}
	for header := range defaultExcludedHeaders {
		httpHeadersToExclude[header] = true
	}
	for _, header := range headersList {
		httpHeadersToExclude[header] = true
	}

	return Log{
		Log:                   log,
		LogRequestHeaders:     logRequestHeaders,
		LogRequestAtInfoLevel: logRequestAtInfoLevel,
		SourceIPs:             sourceIPs,
		HTTPHeadersToExclude:  httpHeadersToExclude,
	}
}

// logWithRequest information from the request and context as fields.
func (l Log) logWithRequest(r *http.Request) log.Interface {
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
		headers, err := dumpRequest(r, l.HTTPHeadersToExclude)
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

		switch {
		// success and shouldn't log successful requests.
		case statusCode >= 200 && statusCode < 300 && l.DisableRequestSuccessLog:
			return

		case 100 <= statusCode && statusCode < 500 || statusCode == http.StatusBadGateway || statusCode == http.StatusServiceUnavailable:
			if l.LogRequestAtInfoLevel {
				requestLog.Infof("%s %s (%d) %s", r.Method, uri, statusCode, time.Since(begin))

				if l.LogRequestHeaders && headers != nil {
					requestLog.Infof("ws: %v; %s", IsWSHandshakeRequest(r), string(headers))
				}
				return
			}

			requestLog.Debugf("%s %s (%d) %s", r.Method, uri, statusCode, time.Since(begin))
			if l.LogRequestHeaders && headers != nil {
				requestLog.Debugf("ws: %v; %s", IsWSHandshakeRequest(r), string(headers))
			}
		default:
			requestLog.Warnf("%s %s (%d) %s Response: %q ws: %v; %s",
				r.Method, uri, statusCode, time.Since(begin), buf.Bytes(), IsWSHandshakeRequest(r), headers)
		}
	})
}

// Logging middleware logs each HTTP request method, path, response code and
// duration for all HTTP requests.
var Logging = Log{
	Log: log.Global(),
}

func dumpRequest(req *http.Request, httpHeadersToExclude map[string]bool) ([]byte, error) {
	var b bytes.Buffer

	// In case users initialize the Log middleware using the exported struct, skip the default headers anyway
	if len(httpHeadersToExclude) == 0 {
		httpHeadersToExclude = defaultExcludedHeaders
	}
	// Exclude some headers for security, or just that we don't need them when debugging
	err := req.Header.WriteSubset(&b, httpHeadersToExclude)
	if err != nil {
		return nil, err
	}

	ret := bytes.Replace(b.Bytes(), []byte("\r\n"), []byte("; "), -1)
	return ret, nil
}
