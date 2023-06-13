package distributor

import (
	"net/http"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/validation"
)

var (
	testStreamLabels = "FIXME"
	testTime         = time.Now()
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
			httpgrpc.Errorf(
				http.StatusBadRequest,
				validation.GreaterThanMaxSampleAgeErrorMsg,
				testStreamLabels,
				testTime.Add(-time.Hour*5).Format(timeFormat),
				testTime.Add(-1*time.Hour).Format(timeFormat), // same as RejectOldSamplesMaxAge
			),
		},
		{
			"test too new",
			"test",
			nil,
			logproto.Entry{Timestamp: testTime.Add(time.Hour * 5), Line: "test"},
			httpgrpc.Errorf(http.StatusBadRequest, validation.TooFarInFutureErrorMsg, testStreamLabels, testTime.Add(time.Hour*5).Format(timeFormat)),
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
			httpgrpc.Errorf(http.StatusBadRequest, validation.LineTooLongErrorMsg, 10, testStreamLabels, 11),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &validation.Limits{}
			flagext.DefaultValues(l)
			o, err := validation.NewOverrides(*l, tt.overrides)
			assert.NoError(t, err)
			v, err := NewValidator(o)
			assert.NoError(t, err)

			err = v.ValidateEntry(v.getValidationContextForTime(testTime, tt.userID), testStreamLabels, tt.entry)
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
			httpgrpc.Errorf(http.StatusBadRequest, validation.MissingLabelsErrorMsg),
		},
		{
			"test too many labels",
			"test",
			fakeLimits{
				&validation.Limits{MaxLabelNamesPerSeries: 2},
			},
			"{foo=\"bar\",food=\"bars\",fed=\"bears\"}",
			httpgrpc.Errorf(http.StatusBadRequest, validation.MaxLabelNamesPerSeriesErrorMsg, "{foo=\"bar\",food=\"bars\",fed=\"bears\"}", 3, 2),
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
			httpgrpc.Errorf(http.StatusBadRequest, validation.LabelNameTooLongErrorMsg, "{fooooo=\"bar\"}", "fooooo"),
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
			httpgrpc.Errorf(http.StatusBadRequest, validation.LabelValueTooLongErrorMsg, "{foo=\"barrrrrr\"}", "barrrrrr"),
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
			httpgrpc.Errorf(http.StatusBadRequest, validation.DuplicateLabelNamesErrorMsg, "{foo=\"bar\", foo=\"barf\"}", "foo"),
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
			httpgrpc.ErrorFromHTTPResponse(&httpgrpc.HTTPResponse{
				Code: int32(http.StatusBadRequest),
				Body: []byte("stream '{foo=\"bar\", foo=\"barf%s\"}' has label value too long: 'barf%s'"), // Intentionally construct the string to make sure %s isn't substituted as (MISSING)
			}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &validation.Limits{}
			flagext.DefaultValues(l)
			o, err := validation.NewOverrides(*l, tt.overrides)
			assert.NoError(t, err)
			v, err := NewValidator(o)
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
