package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/util/validation"
)

// wrapLimit mirrors how pkg/querier/queryrange/limits.go builds its rejections:
// a sentinel wrapping the client-facing httpgrpc error.
func wrapLimit(sentinel error, status int, msg string) error {
	return fmt.Errorf("%w: %w", sentinel, httpgrpc.Errorf(status, "%s", msg))
}

func TestClassifyFailure(t *testing.T) {
	for _, tc := range []struct {
		name         string
		err          error
		wantCategory string
		wantReason   string
	}{
		{"nil", nil, "", ""},
		{"canceled", context.Canceled, FailureCanceled, "client_canceled"},
		{"deadline", context.DeadlineExceeded, FailureTimeout, "query_timeout"},
		{
			"max_query_bytes_read",
			wrapLimit(logqlmodel.ErrMaxQueryBytesRead, http.StatusBadRequest, "the query would read too many bytes (query: 5GB, limit: 1GB)"),
			FailureLimit, "max_query_bytes_read",
		},
		{
			"querier_too_large",
			wrapLimit(logqlmodel.ErrQuerierTooManyBytes, http.StatusBadRequest, "query too large to execute on a single querier: (query: 5GB, limit: 1GB)"),
			FailureLimit, "querier_too_large",
		},
		{"interval_limit", logqlmodel.ErrIntervalLimit, FailureLimit, "interval_limit"},
		{"series_limit", logqlmodel.NewSeriesLimitError(100), FailureLimit, "series_limit"},
		{"blocked", logqlmodel.ErrBlocked, FailureBlocked, "blocked_by_policy"},
		{"parse", logqlmodel.ErrParse, FailureSyntax, "parse"},
		{"pipeline", logqlmodel.ErrPipeline, FailureSyntax, "pipeline"},
		{"no_org_id", user.ErrNoOrgID, FailureUserError, "no_org_id"},
		{
			// Raised in-process, so it is matched by sentinel.
			"max_entries",
			wrapLimit(logqlmodel.ErrMaxEntriesLimit, http.StatusBadRequest, fmt.Sprintf(validation.ErrMaxEntriesLimit, 10000, 5000)),
			FailureLimit, "max_entries",
		},
		{
			// Raised in-process, so it is matched by sentinel.
			"max_query_length",
			wrapLimit(logqlmodel.ErrMaxQueryLength, http.StatusBadRequest, fmt.Sprintf(validation.ErrQueryTooLong, "800h", "721h")),
			FailureLimit, "max_query_length",
		},
		{
			// The same limit raised on a querier or by the v2 engine arrives with
			// no error identity left, so the message-match fallback has to
			// produce the same category and reason as the sentinel case above.
			"max_entries_400_message_only",
			httpgrpc.Errorf(http.StatusBadRequest, validation.ErrMaxEntriesLimit, 10000, 5000),
			FailureLimit, "max_entries",
		},
		{
			// Wire-crossing counterpart of max_query_length, as above.
			"max_query_length_400_message_only",
			httpgrpc.Errorf(http.StatusBadRequest, validation.ErrQueryTooLong, "800h", "721h"),
			FailureLimit, "max_query_length",
		},
		{
			"gateway_timeout",
			httpgrpc.Errorf(http.StatusGatewayTimeout, "upstream timeout"),
			FailureTimeout, "query_timeout",
		},
		{
			"too_many_requests_generic",
			httpgrpc.Errorf(http.StatusTooManyRequests, "rate limited"),
			FailureThrottled, "too_many_requests",
		},
		{
			// A 429 whose sentinel did not survive the wire is bucketed
			// generically: the message is deliberately not matched.
			"max_query_parallelism_message_only",
			httpgrpc.Errorf(http.StatusTooManyRequests, "%s", logqlmodel.ErrMaxQueryParallelism.Error()),
			FailureThrottled, "too_many_requests",
		},
		{
			// Raised in-process, so it is matched by sentinel.
			"max_query_parallelism",
			wrapLimit(logqlmodel.ErrMaxQueryParallelism, http.StatusTooManyRequests, logqlmodel.ErrMaxQueryParallelism.Error()),
			FailureThrottled, "max_query_parallelism",
		},
		{
			// An unrelated 400 must not be over-matched onto a limit reason above.
			"unrelated_bad_request",
			httpgrpc.Errorf(http.StatusBadRequest, "some unrelated bad request"),
			FailureUserError, "bad_request",
		},
		{
			"internal",
			httpgrpc.Errorf(http.StatusInternalServerError, "too many unhealthy instances in the ring"),
			FailureInternal, "downstream_error",
		},
		{"unknown_plain", fmt.Errorf("some unexpected error"), FailureInternal, "downstream_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotCategory, gotReason := ClassifyFailure(tc.err)
			require.Equal(t, tc.wantCategory, gotCategory, "category")
			require.Equal(t, tc.wantReason, gotReason, "reason")
		})
	}
}

// TestWrappedLimitErrorStatusPreserved guards the sentinel-wrapping invariant:
// wrapping must not change the status or message the client sees.
func TestWrappedLimitErrorStatusPreserved(t *testing.T) {
	for _, tc := range []struct {
		name     string
		sentinel error
		status   int
		msg      string
	}{
		{
			"max_query_bytes_read",
			logqlmodel.ErrMaxQueryBytesRead,
			http.StatusBadRequest,
			"the query would read too many bytes (query: 5GB, limit: 1GB)",
		},
		{
			"max_query_length",
			logqlmodel.ErrMaxQueryLength,
			http.StatusBadRequest,
			fmt.Sprintf(validation.ErrQueryTooLong, "800h", "721h"),
		},
		{
			"max_entries",
			logqlmodel.ErrMaxEntriesLimit,
			http.StatusBadRequest,
			fmt.Sprintf(validation.ErrMaxEntriesLimit, 10000, 5000),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := wrapLimit(tc.sentinel, tc.status, tc.msg)

			status, clientErr := ClientHTTPStatusAndError(wrapped)
			require.Equal(t, tc.status, status)
			// The message comes from the embedded HTTPResponse body, not the
			// sentinel prefix.
			require.Equal(t, tc.msg, clientErr.Error())
			// And the sentinel is still detectable for classification.
			require.ErrorIs(t, wrapped, tc.sentinel)
		})
	}
}

// TestPrefixBeforeFormat guards the assumption reasonForBadRequest relies on:
// every template it matches against has a non-empty, distinct stable prefix.
// Matching on an empty prefix would classify every 400 as that reason.
func TestPrefixBeforeFormat(t *testing.T) {
	templates := map[string]string{
		"max_entries":      validation.ErrMaxEntriesLimit,
		"max_query_length": validation.ErrQueryTooLong,
	}

	seen := make(map[string]string, len(templates))
	for name, tmpl := range templates {
		prefix := prefixBeforeFormat(tmpl)
		require.NotEmpty(t, prefix, "template %q has no stable prefix to match on", name)
		require.NotContains(t, prefix, "%", "prefix for %q must not contain a format verb", name)

		if other, ok := seen[prefix]; ok {
			t.Fatalf("templates %q and %q share the prefix %q", name, other, prefix)
		}
		seen[prefix] = name

		// A formatted instance of the template must be recognised, and no other
		// template's instance may be.
		msg := strings.ReplaceAll(strings.ReplaceAll(tmpl, "%s", "x"), "%d", "1")
		require.True(t, matchesTemplate(msg, tmpl))
		for otherName, otherTmpl := range templates {
			if otherName == name {
				continue
			}
			require.False(t, matchesTemplate(msg, otherTmpl), "%q must not match the %q template", name, otherName)
		}
	}

	// A verb-first template has no stable prefix and must not match anything.
	require.Empty(t, prefixBeforeFormat("%s exceeded the limit"))
	require.False(t, matchesTemplate("any message at all", "%s exceeded the limit"))
}
