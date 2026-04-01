package urlhelper

import (
	"fmt"
	"net/http"
)

func GetBaseURL(r *http.Request) string {
	scheme := getScheme(r)
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}

func getScheme(r *http.Request) string {
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return proto
	}
	if r.TLS != nil {
		return "https"
	}
	if r.URL.Scheme != "" {
		return r.URL.Scheme
	}
	return "http"
}
