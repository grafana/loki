package validation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

func Test_PolicyStreamMapping_PolicyFor(t *testing.T) {
	mapping := PolicyStreamMapping{
		"policy1": []*PriorityStream{
			{
				Selector: `{foo="bar"}`,
				Priority: 2,
				Matchers: []*labels.Matcher{
					labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"),
				},
			},
		},
		"policy2": []*PriorityStream{
			{
				Selector: `{foo="bar", daz="baz"}`,
				Priority: 1,
				Matchers: []*labels.Matcher{
					labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"),
					labels.MustNewMatcher(labels.MatchEqual, "daz", "baz"),
				},
			},
		},
		"policy3": []*PriorityStream{
			{
				Selector: `{qyx="qzx", qox="qox"}`,
				Priority: 2,
				Matchers: []*labels.Matcher{
					labels.MustNewMatcher(labels.MatchEqual, "qyx", "qzx"),
					labels.MustNewMatcher(labels.MatchEqual, "qox", "qox"),
				},
			},
		},
		"policy4": []*PriorityStream{
			{
				Selector: `{qyx="qzx", qox="qox"}`,
				Priority: 1,
				Matchers: []*labels.Matcher{
					labels.MustNewMatcher(labels.MatchEqual, "qyx", "qzx"),
					labels.MustNewMatcher(labels.MatchEqual, "qox", "qox"),
				},
			},
		},
		"policy5": []*PriorityStream{
			{
				Selector: `{qab=~"qzx.*"}`,
				Priority: 2,
				Matchers: []*labels.Matcher{
					labels.MustNewMatcher(labels.MatchRegexp, "qab", "qzx.*"),
				},
			},
		},
		"policy6": []*PriorityStream{
			{
				Selector: `{env="prod"}`,
				Priority: 2,
				Matchers: []*labels.Matcher{
					labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
				},
			},
			{
				Selector: `{env=~"prod|staging"}`,
				Priority: 1,
				Matchers: []*labels.Matcher{
					labels.MustNewMatcher(labels.MatchRegexp, "env", "prod|staging"),
				},
			},
			{
				Selector: `{team="finance"}`,
				Priority: 4,
				Matchers: []*labels.Matcher{
					labels.MustNewMatcher(labels.MatchEqual, "team", "finance"),
				},
			},
		},
		"policy7": []*PriorityStream{
			{
				Selector: `{env=~"prod|dev"}`,
				Priority: 3,
				Matchers: []*labels.Matcher{
					labels.MustNewMatcher(labels.MatchRegexp, "env", "prod|dev"),
				},
			},
		},
		"policy8": []*PriorityStream{
			{
				Selector: `{env=~"prod|test"}`,
				Priority: 3,
				Matchers: []*labels.Matcher{
					labels.MustNewMatcher(labels.MatchRegexp, "env", "prod|test"),
				},
			},
		},
	}

	require.NoError(t, mapping.Validate())

	ctx := t.Context()
	require.Equal(t, []string{"policy1"}, mapping.PolicyFor(ctx, labels.FromStrings("foo", "bar")))
	// matches both policy2 and policy1 but policy1 has higher priority.
	require.Equal(t, []string{"policy1"}, mapping.PolicyFor(ctx, labels.FromStrings("foo", "bar", "daz", "baz")))
	// matches policy3 and policy4 but policy3 has higher priority..
	require.Equal(t, []string{"policy3"}, mapping.PolicyFor(ctx, labels.FromStrings("qyx", "qzx", "qox", "qox")))
	// matches no policy.
	require.Empty(t, mapping.PolicyFor(ctx, labels.FromStrings("foo", "fooz", "daz", "qux", "quux", "corge")))
	// matches policy5 through regex.
	require.Equal(t, []string{"policy5"}, mapping.PolicyFor(ctx, labels.FromStrings("qab", "qzxqox")))

	require.Equal(t, []string{"policy6"}, mapping.PolicyFor(ctx, labels.FromStrings("env", "prod", "team", "finance")))
	// Matches policy7 and policy8 which have the same priority.
	require.Equal(t, []string{"policy7", "policy8"}, mapping.PolicyFor(ctx, labels.FromStrings("env", "prod")))
}

func TestPolicyStreamMapping_ApplyDefaultPolicyStreamMappings(t *testing.T) {
	tests := []struct {
		name          string
		existing      PolicyStreamMapping
		defaults      PolicyStreamMapping
		expected      PolicyStreamMapping
		expectedError bool
	}{
		{
			name:     "nil existing, nil defaults",
			existing: nil,
			defaults: nil,
			expected: nil,
		},
		{
			name:     "nil existing, with defaults",
			existing: nil,
			defaults: PolicyStreamMapping{
				"policy1": {
					{Priority: 1, Selector: "{app=\"test\"}"},
				},
			},
			expected: PolicyStreamMapping{
				"policy1": {
					{Priority: 1, Selector: "{app=\"test\"}"},
				},
			},
		},
		{
			name: "existing with defaults, no overlap",
			existing: PolicyStreamMapping{
				"policy1": {
					{Priority: 2, Selector: "{app=\"existing\"}"},
				},
			},
			defaults: PolicyStreamMapping{
				"policy2": {
					{Priority: 1, Selector: "{app=\"default\"}"},
				},
			},
			expected: PolicyStreamMapping{
				"policy1": {
					{Priority: 2, Selector: "{app=\"existing\"}"},
				},
				"policy2": {
					{Priority: 1, Selector: "{app=\"default\"}"},
				},
			},
		},
		{
			name: "existing with defaults, with overlap - existing takes precedence",
			existing: PolicyStreamMapping{
				"policy1": {
					{Priority: 2, Selector: "{app=\"existing\"}"},
				},
			},
			defaults: PolicyStreamMapping{
				"policy1": {
					{Priority: 1, Selector: "{app=\"existing\"}"}, // Same selector, different priority
				},
				"policy2": {
					{Priority: 1, Selector: "{app=\"default\"}"},
				},
			},
			expected: PolicyStreamMapping{
				"policy1": {
					{Priority: 2, Selector: "{app=\"existing\"}"}, // Existing priority preserved
				},
				"policy2": {
					{Priority: 1, Selector: "{app=\"default\"}"},
				},
			},
		},
		{
			name: "existing with defaults, merge different selectors",
			existing: PolicyStreamMapping{
				"policy1": {
					{Priority: 2, Selector: "{app=\"existing\"}"},
				},
			},
			defaults: PolicyStreamMapping{
				"policy1": {
					{Priority: 1, Selector: "{app=\"default\"}"}, // Different selector
				},
			},
			expected: PolicyStreamMapping{
				"policy1": {
					{Priority: 2, Selector: "{app=\"existing\"}"},
					{Priority: 1, Selector: "{app=\"default\"}"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a copy of existing for the test
			var existingCopy PolicyStreamMapping
			if tt.existing != nil {
				existingCopy = make(PolicyStreamMapping)
				for k, v := range tt.existing {
					streamsCopy := make([]*PriorityStream, len(v))
					for i, stream := range v {
						streamsCopy[i] = &PriorityStream{
							Priority: stream.Priority,
							Selector: stream.Selector,
							Matchers: stream.Matchers,
						}
					}
					existingCopy[k] = streamsCopy
				}
			}

			// Apply defaults
			err := existingCopy.ApplyDefaultPolicyStreamMappings(tt.defaults)
			if tt.expectedError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Validate the result
			if tt.expected == nil {
				require.Nil(t, existingCopy)
			} else {
				require.NotNil(t, existingCopy)
				require.Equal(t, len(tt.expected), len(existingCopy))

				for policyName, expectedStreams := range tt.expected {
					actualStreams, exists := existingCopy[policyName]
					require.True(t, exists, "Policy %s should exist", policyName)
					require.Equal(t, len(expectedStreams), len(actualStreams))

					// Check each stream
					for i, expectedStream := range expectedStreams {
						require.Less(t, i, len(actualStreams))
						actualStream := actualStreams[i]
						require.Equal(t, expectedStream.Priority, actualStream.Priority)
						require.Equal(t, expectedStream.Selector, actualStream.Selector)
					}
				}
			}
		})
	}
}

func TestPolicyStreamMapping_ApplyDefaultPolicyStreamMappings_Validation(t *testing.T) {
	// Test that validation is called after merging
	existing := PolicyStreamMapping{
		"policy1": {
			{Priority: 2, Selector: "{app=\"existing\"}"},
		},
	}

	defaults := PolicyStreamMapping{
		"policy1": {
			{Priority: 1, Selector: "{app=\"default\"}"},
		},
	}

	// This should not panic and should call Validate()
	err := existing.ApplyDefaultPolicyStreamMappings(defaults)
	require.NoError(t, err)

	// Verify the result is valid
	require.NoError(t, existing.Validate())
}

func Test_PolicyStreamMapping_PolicyFor_WithHeaderOverride(t *testing.T) {
	mapping := PolicyStreamMapping{
		"policy1": []*PriorityStream{
			{
				Selector: `{foo="bar"}`,
				Priority: 2,
				Matchers: []*labels.Matcher{
					labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"),
				},
			},
		},
		"policy2": []*PriorityStream{
			{
				Selector: `{env="prod"}`,
				Priority: 1,
				Matchers: []*labels.Matcher{
					labels.MustNewMatcher(labels.MatchEqual, "env", "prod"),
				},
			},
		},
	}

	require.NoError(t, mapping.Validate())

	t.Run("without header context, uses normal mapping", func(t *testing.T) {
		ctx := t.Context()
		// Should match policy1 based on labels
		require.Equal(t, []string{"policy1"}, mapping.PolicyFor(ctx, labels.FromStrings("foo", "bar")))
		// Should match policy2 based on labels
		require.Equal(t, []string{"policy2"}, mapping.PolicyFor(ctx, labels.FromStrings("env", "prod")))
		// Should match no policy
		require.Empty(t, mapping.PolicyFor(ctx, labels.FromStrings("unknown", "label")))
	})

	t.Run("with header context, overrides all mappings", func(t *testing.T) {
		ctx := InjectIngestionPolicyContext(t.Context(), "override-policy")

		// Even though labels match policy1, header policy overrides
		require.Equal(t, []string{"override-policy"}, mapping.PolicyFor(ctx, labels.FromStrings("foo", "bar")))

		// Even though labels match policy2, header policy overrides
		require.Equal(t, []string{"override-policy"}, mapping.PolicyFor(ctx, labels.FromStrings("env", "prod")))

		// Even though labels don't match anything, header policy is used
		require.Equal(t, []string{"override-policy"}, mapping.PolicyFor(ctx, labels.FromStrings("unknown", "label")))
	})

	t.Run("empty header context is ignored", func(t *testing.T) {
		// Inject empty string - should be treated as not set
		ctx := InjectIngestionPolicyContext(t.Context(), "")

		// Should fall back to normal mapping behavior
		require.Equal(t, []string{"policy1"}, mapping.PolicyFor(ctx, labels.FromStrings("foo", "bar")))
	})
}

func TestExtractInjectIngestionPolicyContext(t *testing.T) {
	t.Run("inject and extract policy", func(t *testing.T) {
		policy := "test-policy"

		ctx := InjectIngestionPolicyContext(t.Context(), policy)
		extracted := ExtractIngestionPolicyContext(ctx)
		require.Equal(t, policy, extracted)
	})

	t.Run("extract from empty context", func(t *testing.T) {
		extracted := ExtractIngestionPolicyContext(t.Context())
		require.Empty(t, extracted)
	})

	t.Run("inject empty string", func(t *testing.T) {
		ctx := InjectIngestionPolicyContext(t.Context(), "")
		extracted := ExtractIngestionPolicyContext(ctx)
		require.Empty(t, extracted)
	})
}

func TestExtractIngestionPolicyHTTP(t *testing.T) {
	t.Run("extract policy from header", func(t *testing.T) {
		req, err := http.NewRequest("POST", "/loki/api/v1/push", nil)
		require.NoError(t, err)

		req.Header.Set(HTTPHeaderIngestionPolicyKey, "my-policy")

		policy := ExtractIngestionPolicyHTTP(req)
		require.Equal(t, "my-policy", policy)
	})

	t.Run("no header present", func(t *testing.T) {
		req, err := http.NewRequest("POST", "/loki/api/v1/push", nil)
		require.NoError(t, err)

		policy := ExtractIngestionPolicyHTTP(req)
		require.Empty(t, policy)
	})

	t.Run("empty header value", func(t *testing.T) {
		req, err := http.NewRequest("POST", "/loki/api/v1/push", nil)
		require.NoError(t, err)

		req.Header.Set(HTTPHeaderIngestionPolicyKey, "")

		policy := ExtractIngestionPolicyHTTP(req)
		require.Empty(t, policy)
	})

	t.Run("header with whitespace", func(t *testing.T) {
		req, err := http.NewRequest("POST", "/loki/api/v1/push", nil)
		require.NoError(t, err)

		req.Header.Set(HTTPHeaderIngestionPolicyKey, "  policy-with-spaces  ")

		policy := ExtractIngestionPolicyHTTP(req)
		require.Equal(t, "  policy-with-spaces  ", policy)
	})
}

func TestIngestionPolicyMiddleware(t *testing.T) {
	t.Run("middleware injects policy into context", func(t *testing.T) {
		var capturedCtx context.Context
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedCtx = r.Context()
			w.WriteHeader(http.StatusOK)
		})

		middleware := NewIngestionPolicyMiddleware(nil)
		wrappedHandler := middleware.Wrap(handler)

		req, err := http.NewRequest("POST", "/loki/api/v1/push", nil)
		require.NoError(t, err)
		req.Header.Set(HTTPHeaderIngestionPolicyKey, "test-policy")

		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		policy := ExtractIngestionPolicyContext(capturedCtx)
		require.Equal(t, "test-policy", policy)
	})

	t.Run("middleware does not modify context when no header", func(t *testing.T) {
		var capturedCtx context.Context
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedCtx = r.Context()
			w.WriteHeader(http.StatusOK)
		})

		middleware := NewIngestionPolicyMiddleware(nil)
		wrappedHandler := middleware.Wrap(handler)

		req, err := http.NewRequest("POST", "/loki/api/v1/push", nil)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		policy := ExtractIngestionPolicyContext(capturedCtx)
		require.Empty(t, policy)
	})

	t.Run("middleware does not inject empty header value", func(t *testing.T) {
		var capturedCtx context.Context
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedCtx = r.Context()
			w.WriteHeader(http.StatusOK)
		})

		middleware := NewIngestionPolicyMiddleware(nil)
		wrappedHandler := middleware.Wrap(handler)

		req, err := http.NewRequest("POST", "/loki/api/v1/push", nil)
		require.NoError(t, err)
		req.Header.Set(HTTPHeaderIngestionPolicyKey, "")

		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		policy := ExtractIngestionPolicyContext(capturedCtx)
		require.Empty(t, policy)
	})
}

func TestGRPCIngestionPolicy(t *testing.T) {
	t.Run("inject and extract policy through gRPC", func(t *testing.T) {
		policy := "test-policy"

		// Inject policy into context
		ctx := InjectIngestionPolicyContext(t.Context(), policy)

		// Inject into gRPC metadata
		ctx, err := injectIntoGRPCRequest(ctx)
		require.NoError(t, err)

		// Extract from gRPC metadata
		ctx2, err := extractFromGRPCRequest(ctx)
		require.NoError(t, err)

		// Verify extracted policy matches original
		extractedPolicy := ExtractIngestionPolicyContext(ctx2)
		require.Equal(t, policy, extractedPolicy)
	})

	t.Run("extract from empty context returns empty", func(t *testing.T) {
		ctx, err := extractFromGRPCRequest(t.Context())
		require.NoError(t, err)

		policy := ExtractIngestionPolicyContext(ctx)
		require.Empty(t, policy)
	})

	t.Run("inject empty policy does not add metadata", func(t *testing.T) {
		// Inject empty policy into context
		ctx := InjectIngestionPolicyContext(t.Context(), "")

		// Try to inject into gRPC metadata
		ctx, err := injectIntoGRPCRequest(ctx)
		require.NoError(t, err)

		// Extract from gRPC metadata
		ctx2, err := extractFromGRPCRequest(ctx)
		require.NoError(t, err)

		// Should still be empty
		policy := ExtractIngestionPolicyContext(ctx2)
		require.Empty(t, policy)
	})

	t.Run("inject context without policy does nothing", func(t *testing.T) {
		// Try to inject into gRPC metadata (no policy in context)
		ctx, err := injectIntoGRPCRequest(t.Context())
		require.NoError(t, err)

		// Extract from gRPC metadata
		ctx2, err := extractFromGRPCRequest(ctx)
		require.NoError(t, err)

		// Should be empty
		policy := ExtractIngestionPolicyContext(ctx2)
		require.Empty(t, policy)
	})
}

func TestKafkaIngestionPolicyRoundtrip(t *testing.T) {
	t.Run("roundtrip with policy", func(t *testing.T) {
		// Start with a context containing a policy
		originalCtx := InjectIngestionPolicyContext(t.Context(), "test-policy")

		// Create records
		records := []*kgo.Record{
			{Value: []byte("record1")},
			{Value: []byte("record2")},
		}

		// Simulate producer side: inject policy into record headers
		err := IngestionPoliciesKafkaProducerInterceptor(originalCtx, records)
		require.NoError(t, err)

		// Verify headers were added to all records
		for _, record := range records {
			require.Len(t, record.Headers, 1)
			require.Equal(t, lowerIngestionPolicyHeaderName, record.Headers[0].Key)
			require.Equal(t, []byte("test-policy"), record.Headers[0].Value)
		}

		// Simulate consumer side: extract policy from record headers back into context
		for _, record := range records {
			consumerCtx := IngestionPoliciesKafkaHeadersToContext(t.Context(), record.Headers)

			// Verify the policy was correctly extracted
			extractedPolicy := ExtractIngestionPolicyContext(consumerCtx)
			require.Equal(t, "test-policy", extractedPolicy)
		}
	})

	t.Run("roundtrip without policy", func(t *testing.T) {
		// Start with a context without a policy
		originalCtx := t.Context()

		// Create records
		records := []*kgo.Record{
			{Value: []byte("record1")},
		}

		// Simulate producer side: no policy to inject
		err := IngestionPoliciesKafkaProducerInterceptor(originalCtx, records)
		require.NoError(t, err)

		// Verify no headers were added
		require.Len(t, records[0].Headers, 0)

		// Simulate consumer side: extract from empty headers
		consumerCtx := IngestionPoliciesKafkaHeadersToContext(t.Context(), records[0].Headers)

		// Verify no policy was extracted
		extractedPolicy := ExtractIngestionPolicyContext(consumerCtx)
		require.Empty(t, extractedPolicy)
	})

	t.Run("roundtrip with empty policy", func(t *testing.T) {
		// Start with a context with an empty policy
		originalCtx := InjectIngestionPolicyContext(t.Context(), "")

		// Create records
		records := []*kgo.Record{
			{Value: []byte("record1")},
		}

		// Simulate producer side: empty policy should not be injected
		err := IngestionPoliciesKafkaProducerInterceptor(originalCtx, records)
		require.NoError(t, err)

		// Verify no headers were added for empty policy
		require.Len(t, records[0].Headers, 0)

		// Simulate consumer side
		consumerCtx := IngestionPoliciesKafkaHeadersToContext(t.Context(), records[0].Headers)

		// Verify no policy was extracted
		extractedPolicy := ExtractIngestionPolicyContext(consumerCtx)
		require.Empty(t, extractedPolicy)
	})

	t.Run("roundtrip with existing headers", func(t *testing.T) {
		// Start with a context containing a policy
		originalCtx := InjectIngestionPolicyContext(t.Context(), "test-policy")

		// Create records with existing headers
		records := []*kgo.Record{
			{
				Value: []byte("record1"),
				Headers: []kgo.RecordHeader{
					{Key: "existing-header", Value: []byte("existing-value")},
				},
			},
		}

		// Simulate producer side: inject policy into record headers
		err := IngestionPoliciesKafkaProducerInterceptor(originalCtx, records)
		require.NoError(t, err)

		// Verify both headers are present
		require.Len(t, records[0].Headers, 2)
		require.Equal(t, "existing-header", records[0].Headers[0].Key)
		require.Equal(t, lowerIngestionPolicyHeaderName, records[0].Headers[1].Key)

		// Simulate consumer side: extract policy from headers
		consumerCtx := IngestionPoliciesKafkaHeadersToContext(t.Context(), records[0].Headers)

		// Verify the policy was correctly extracted despite other headers
		extractedPolicy := ExtractIngestionPolicyContext(consumerCtx)
		require.Equal(t, "test-policy", extractedPolicy)
	})
}
