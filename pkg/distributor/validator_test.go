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

			err = v.ValidateEntry(ctx, v.getValidationContextForTime(testTime, tt.userID), testStreamLabels, tt.entry)
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
			o, err := validation.NewOverrides(*l, tt.overrides)
			assert.NoError(t, err)
			v, err := NewValidator(o, nil)
			assert.NoError(t, err)

			err = v.ValidateLabels(v.getValidationContextForTime(testTime, tt.userID), mustParseLabels(tt.labels), logproto.Stream{Labels: tt.labels})
			assert.Equal(t, tt.expected, err)
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
