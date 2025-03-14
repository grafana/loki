package distributor

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/validation"
)

var (
	testStreamLabels       = labels.Labels{{Name: "my", Value: "label"}}
	testStreamLabelsString = testStreamLabels.String()
	testTime               = time.Now()
)

type fakeLimits struct {
	limits *validation.Limits
}

func (f fakeLimits) TenantLimits(_ string) *validation.Limits {
	return f.limits
}

// unused, but satisfies interface
func (f fakeLimits) AllByUserID() map[string]*validation.Limits {
	return nil
}

func TestValidator_ValidateEntry(t *testing.T) {
	tests := []struct {
		name      string
		userID    string
		overrides validation.TenantLimits
		entry     logproto.Entry
		expected  error
	}{
		{
			"test valid",
			"test",
			nil,
			logproto.Entry{Timestamp: testTime, Line: "test"},
			nil,
		},
		{
			"test too old",
			"test",
			fakeLimits{
				&validation.Limits{
					RejectOldSamples:       true,
					RejectOldSamplesMaxAge: model.Duration(1 * time.Hour),
				},
			},
			logproto.Entry{Timestamp: testTime.Add(-time.Hour * 5), Line: "test"},
			fmt.Errorf(validation.GreaterThanMaxSampleAgeErrorMsg,
				testStreamLabelsString,
				testTime.Add(-time.Hour*5).Format(timeFormat),
				testTime.Add(-1*time.Hour).Format(timeFormat), // same as RejectOldSamplesMaxAge
			),
		},
		{
			"test too new",
			"test",
			nil,
			logproto.Entry{Timestamp: testTime.Add(time.Hour * 5), Line: "test"},
			fmt.Errorf(validation.TooFarInFutureErrorMsg, testStreamLabelsString, testTime.Add(time.Hour*5).Format(timeFormat)),
		},
		{
			"line too long",
			"test",
			fakeLimits{
				&validation.Limits{
					MaxLineSize: 10,
				},
			},
			logproto.Entry{Timestamp: testTime, Line: "12345678901"},
			fmt.Errorf(validation.LineTooLongErrorMsg, 10, testStreamLabelsString, 11),
		},
		{
			"disallowed structured metadata",
			"test",
			fakeLimits{
				&validation.Limits{
					AllowStructuredMetadata: false,
				},
			},
			logproto.Entry{Timestamp: testTime, Line: "12345678901", StructuredMetadata: push.LabelsAdapter{{Name: "foo", Value: "bar"}}},
			fmt.Errorf(validation.DisallowedStructuredMetadataErrorMsg, testStreamLabelsString),
		},
		{
			"structured metadata too big",
			"test",
			fakeLimits{
				&validation.Limits{
					AllowStructuredMetadata:   true,
					MaxStructuredMetadataSize: 4,
				},
			},
			logproto.Entry{Timestamp: testTime, Line: "12345678901", StructuredMetadata: push.LabelsAdapter{{Name: "foo", Value: "bar"}}},
			fmt.Errorf(validation.StructuredMetadataTooLargeErrorMsg, testStreamLabelsString, 6, 4),
		},
		{
			"structured metadata too many",
			"test",
			fakeLimits{
				&validation.Limits{
					AllowStructuredMetadata:           true,
					MaxStructuredMetadataEntriesCount: 1,
				},
			},
			logproto.Entry{Timestamp: testTime, Line: "12345678901", StructuredMetadata: push.LabelsAdapter{{Name: "foo", Value: "bar"}, {Name: "too", Value: "many"}}},
			fmt.Errorf(validation.StructuredMetadataTooManyErrorMsg, testStreamLabelsString, 2, 1),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &validation.Limits{}
			flagext.DefaultValues(l)
			o, err := validation.NewOverrides(*l, tt.overrides)
			assert.NoError(t, err)
			v, err := NewValidator(o, nil)
			assert.NoError(t, err)
			retentionHours := util.RetentionHours(v.RetentionPeriod(tt.userID))

			err = v.ValidateEntry(ctx, v.getValidationContextForTime(testTime, tt.userID), testStreamLabels, tt.entry, retentionHours, "")
			assert.Equal(t, tt.expected, err)
		})
	}
}

func TestValidator_ValidateLabels(t *testing.T) {
	tests := []struct {
		name      string
		userID    string
		overrides validation.TenantLimits
		labels    string
		expected  error
	}{
		{
			"test valid",
			"test",
			nil,
			"{foo=\"bar\"}",
			nil,
		},
		{
			"empty",
			"test",
			nil,
			"{}",
			fmt.Errorf(validation.MissingLabelsErrorMsg),
		},
		{
			"test too many labels",
			"test",
			fakeLimits{
				&validation.Limits{MaxLabelNamesPerSeries: 2},
			},
			"{foo=\"bar\",food=\"bars\",fed=\"bears\"}",
			fmt.Errorf(validation.MaxLabelNamesPerSeriesErrorMsg, "{foo=\"bar\",food=\"bars\",fed=\"bears\"}", 3, 2),
		},
		{
			"label name too long",
			"test",
			fakeLimits{
				&validation.Limits{
					MaxLabelNamesPerSeries: 2,
					MaxLabelNameLength:     5,
				},
			},
			"{fooooo=\"bar\"}",
			fmt.Errorf(validation.LabelNameTooLongErrorMsg, "{fooooo=\"bar\"}", "fooooo"),
		},
		{
			"label value too long",
			"test",
			fakeLimits{
				&validation.Limits{
					MaxLabelNamesPerSeries: 2,
					MaxLabelNameLength:     5,
					MaxLabelValueLength:    5,
				},
			},
			"{foo=\"barrrrrr\"}",
			fmt.Errorf(validation.LabelValueTooLongErrorMsg, "{foo=\"barrrrrr\"}", "barrrrrr"),
		},
		{
			"duplicate label",
			"test",
			fakeLimits{
				&validation.Limits{
					MaxLabelNamesPerSeries: 2,
					MaxLabelNameLength:     5,
					MaxLabelValueLength:    5,
				},
			},
			"{foo=\"bar\", foo=\"barf\"}",
			fmt.Errorf(validation.DuplicateLabelNamesErrorMsg, "{foo=\"bar\", foo=\"barf\"}", "foo"),
		},
		{
			"label value contains %",
			"test",
			fakeLimits{
				&validation.Limits{
					MaxLabelNamesPerSeries: 2,
					MaxLabelNameLength:     5,
					MaxLabelValueLength:    5,
				},
			},
			"{foo=\"bar\", foo=\"barf%s\"}",
			errors.New("stream '{foo=\"bar\", foo=\"barf%s\"}' has label value too long: 'barf%s'"), // Intentionally construct the string to make sure %s isn't substituted as (MISSING)
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &validation.Limits{}
			flagext.DefaultValues(l)
			retentionHours := util.RetentionHours(time.Duration(l.RetentionPeriod))
			o, err := validation.NewOverrides(*l, tt.overrides)
			assert.NoError(t, err)
			v, err := NewValidator(o, nil)
			assert.NoError(t, err)

			err = v.ValidateLabels(v.getValidationContextForTime(testTime, tt.userID), mustParseLabels(tt.labels), logproto.Stream{Labels: tt.labels}, retentionHours, "")
			assert.Equal(t, tt.expected, err)
		})
	}
}

func TestShouldBlockIngestion(t *testing.T) {
	for _, tc := range []struct {
		name      string
		policy    string
		time      time.Time
		overrides validation.TenantLimits

		expectBlock      bool
		expectStatusCode int
		expectReason     string
	}{
		{
			name: "no block configured",
			time: testTime,
			overrides: fakeLimits{
				&validation.Limits{},
			},
		},
		{
			name:   "all configured tenant blocked priority",
			time:   testTime,
			policy: "policy1",
			overrides: fakeLimits{
				&validation.Limits{
					BlockIngestionUntil: flagext.Time(testTime.Add(time.Hour)),
					BlockIngestionPolicyUntil: map[string]flagext.Time{
						validation.GlobalPolicy: flagext.Time(testTime.Add(-2 * time.Hour)),
						"policy1":               flagext.Time(testTime.Add(-time.Hour)),
					},
					BlockIngestionStatusCode: 1234,
				},
			},
			expectBlock:      true,
			expectStatusCode: 1234,
			expectReason:     validation.BlockedIngestion,
		},
		{
			name:   "named policy priority",
			time:   testTime,
			policy: "policy1",
			overrides: fakeLimits{
				&validation.Limits{
					BlockIngestionUntil: flagext.Time(testTime.Add(-2 * time.Hour)), // Not active anymore
					BlockIngestionPolicyUntil: map[string]flagext.Time{
						validation.GlobalPolicy: flagext.Time(testTime.Add(-time.Hour)),
						"policy1":               flagext.Time(testTime.Add(time.Hour)),
					},
					BlockIngestionStatusCode: 1234,
				},
			},
			expectBlock:      true,
			expectStatusCode: 1234,
			expectReason:     validation.BlockedIngestionPolicy,
		},
		{
			name:   "global policy ignored",
			time:   testTime,
			policy: "policy1",
			overrides: fakeLimits{
				&validation.Limits{
					BlockIngestionUntil: flagext.Time(testTime.Add(-time.Hour)), // Not active anymore
					BlockIngestionPolicyUntil: map[string]flagext.Time{
						validation.GlobalPolicy: flagext.Time(testTime.Add(time.Hour)), // Won't apply since we have a named policy
					},
					BlockIngestionStatusCode: 1234,
				},
			},
			expectBlock: false,
		},
		{
			name:   "global policy matched",
			time:   testTime,
			policy: "", // matches global policy
			overrides: fakeLimits{
				&validation.Limits{
					BlockIngestionPolicyUntil: map[string]flagext.Time{
						validation.GlobalPolicy: flagext.Time(testTime.Add(time.Hour)),
					},
					BlockIngestionStatusCode: 1234,
				},
			},
			expectBlock:      true,
			expectStatusCode: 1234,
			expectReason:     validation.BlockedIngestionPolicy,
		},
		{
			name:   "unknown policy not blocked by global policy",
			time:   testTime,
			policy: "notExists",
			overrides: fakeLimits{
				&validation.Limits{
					BlockIngestionPolicyUntil: map[string]flagext.Time{
						validation.GlobalPolicy: flagext.Time(testTime.Add(time.Hour)),
						"policy1":               flagext.Time(testTime.Add(2 * time.Hour)),
					},
					BlockIngestionStatusCode: 1234,
				},
			},
			expectBlock: false,
		},
		{
			name:   "named policy overrides global policy",
			time:   testTime,
			policy: "policy1",
			overrides: fakeLimits{
				&validation.Limits{
					BlockIngestionPolicyUntil: map[string]flagext.Time{
						validation.GlobalPolicy: flagext.Time(testTime.Add(time.Hour)),
						"policy1":               flagext.Time(testTime.Add(-time.Hour)), // Not blocked overriding block from global quota
					},
					BlockIngestionStatusCode: 1234,
				},
			},
			expectBlock: false,
		},
		{
			name:   "no matching policy",
			time:   testTime,
			policy: "notExists",
			overrides: fakeLimits{
				&validation.Limits{
					BlockIngestionPolicyUntil: map[string]flagext.Time{
						"policy1": flagext.Time(testTime.Add(2 * time.Hour)),
					},
					BlockIngestionStatusCode: 1234,
				},
			},
			expectBlock: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := &validation.Limits{}
			flagext.DefaultValues(l)

			o, err := validation.NewOverrides(*l, tc.overrides)
			assert.NoError(t, err)
			v, err := NewValidator(o, nil)
			assert.NoError(t, err)

			block, statusCode, reason, err := v.ShouldBlockIngestion(v.getValidationContextForTime(testTime, "fake"), testTime, tc.policy)
			assert.Equal(t, tc.expectBlock, block)
			if tc.expectBlock {
				assert.Equal(t, tc.expectStatusCode, statusCode)
				assert.Equal(t, tc.expectReason, reason)
				assert.Error(t, err)
				t.Logf("block: %v, statusCode: %d, reason: %s, err: %v", block, statusCode, reason, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}

}

func mustParseLabels(s string) labels.Labels {
	ls, err := syntax.ParseLabels(s)
	if err != nil {
		panic(err)
	}
	return ls
}
