// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[TimePickerConfig] = (*TimePickerBuilder)(nil)

// Time picker configuration
// It defines the default config for the time picker and the refresh picker for the specific dashboard.
type TimePickerBuilder struct {
	internal *TimePickerConfig
	errors   map[string]cog.BuildErrors
}

func NewTimePickerBuilder() *TimePickerBuilder {
	resource := &TimePickerConfig{}
	builder := &TimePickerBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *TimePickerBuilder) Build() (TimePickerConfig, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("TimePicker", err)...)
	}

	if len(errs) != 0 {
		return TimePickerConfig{}, errs
	}

	return *builder.internal, nil
}

// Whether timepicker is visible or not.
func (builder *TimePickerBuilder) Hidden(hidden bool) *TimePickerBuilder {
	builder.internal.Hidden = &hidden

	return builder
}

// Interval options available in the refresh picker dropdown.
func (builder *TimePickerBuilder) RefreshIntervals(refreshIntervals []string) *TimePickerBuilder {
	builder.internal.RefreshIntervals = refreshIntervals

	return builder
}

// Selectable options available in the time picker dropdown. Has no effect on provisioned dashboard.
func (builder *TimePickerBuilder) TimeOptions(timeOptions []string) *TimePickerBuilder {
	builder.internal.TimeOptions = timeOptions

	return builder
}

// Override the now time by entering a time delay. Use this option to accommodate known delays in data aggregation to avoid null values.
func (builder *TimePickerBuilder) NowDelay(nowDelay string) *TimePickerBuilder {
	builder.internal.NowDelay = &nowDelay

	return builder
}

func (builder *TimePickerBuilder) applyDefaults() {
	builder.Hidden(false)
	builder.RefreshIntervals([]string{"5s", "10s", "30s", "1m", "5m", "15m", "30m", "1h", "2h", "1d"})
	builder.TimeOptions([]string{"5m", "15m", "1h", "6h", "12h", "24h", "2d", "7d", "30d"})
}
