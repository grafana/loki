package distributor

import (
	"net/http"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/assert"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/util/validation"
)

var testStreamLabels = "FIXME"
var testTime = time.Now()

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
			func(userID string) *validation.Limits {
				return &validation.Limits{
					RejectOldSamples:       true,
					RejectOldSamplesMaxAge: 1 * time.Hour,
				}
			},
			logproto.Entry{Timestamp: testTime.Add(-time.Hour * 5), Line: "test"},
			httpgrpc.Errorf(http.StatusBadRequest, validation.GreaterThanMaxSampleAgeErrorMsg(testStreamLabels, testTime.Add(-time.Hour*5))),
		},
		{
			"test too new",
			"test",
			nil,
			logproto.Entry{Timestamp: testTime.Add(time.Hour * 5), Line: "test"},
			httpgrpc.Errorf(http.StatusBadRequest, validation.TooFarInFutureErrorMsg(testStreamLabels, testTime.Add(time.Hour*5))),
		},
		{
			"line too long",
			"test",
			func(userID string) *validation.Limits {
				return &validation.Limits{
					MaxLineSize: 10,
				}
			},
			logproto.Entry{Timestamp: testTime, Line: "12345678901"},
			httpgrpc.Errorf(http.StatusBadRequest, validation.LineTooLongErrorMsg(10, 11, testStreamLabels)),
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

			err = v.ValidateEntry(v.getValidationContextFor(tt.userID), testStreamLabels, tt.entry)
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
			"test too many labels",
			"test",
			func(userID string) *validation.Limits {
				return &validation.Limits{MaxLabelNamesPerSeries: 2}
			},
			"{foo=\"bar\",food=\"bars\",fed=\"bears\"}",
			httpgrpc.Errorf(http.StatusBadRequest, validation.MaxLabelNamesPerSeriesErrorMsg("{foo=\"bar\",food=\"bars\",fed=\"bears\"}", 3, 2)),
		},
		{
			"label name too long",
			"test",
			func(userID string) *validation.Limits {
				return &validation.Limits{
					MaxLabelNamesPerSeries: 2,
					MaxLabelNameLength:     5,
				}
			},
			"{fooooo=\"bar\"}",
			httpgrpc.Errorf(http.StatusBadRequest, validation.LabelNameTooLongErrorMsg("{fooooo=\"bar\"}", "fooooo")),
		},
		{
			"label value too long",
			"test",
			func(userID string) *validation.Limits {
				return &validation.Limits{
					MaxLabelNamesPerSeries: 2,
					MaxLabelNameLength:     5,
					MaxLabelValueLength:    5,
				}
			},
			"{foo=\"barrrrrr\"}",
			httpgrpc.Errorf(http.StatusBadRequest, validation.LabelValueTooLongErrorMsg("{foo=\"barrrrrr\"}", "barrrrrr")),
		},
		{
			"duplicate label",
			"test",
			func(userID string) *validation.Limits {
				return &validation.Limits{
					MaxLabelNamesPerSeries: 2,
					MaxLabelNameLength:     5,
					MaxLabelValueLength:    5,
				}
			},
			"{foo=\"bar\", foo=\"barf\"}",
			httpgrpc.Errorf(http.StatusBadRequest, validation.DuplicateLabelNamesErrorMsg("{foo=\"bar\", foo=\"barf\"}", "foo")),
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

			err = v.ValidateLabels(v.getValidationContextFor(tt.userID), mustParseLabels(tt.labels), logproto.Stream{Labels: tt.labels})
			assert.Equal(t, tt.expected, err)
		})
	}
}

func mustParseLabels(s string) labels.Labels {
	ls, err := logql.ParseLabels(s)
	if err != nil {
		panic(err)
	}
	return ls
}
