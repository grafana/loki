package planner

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/storage/config"
)

type dayRangeIterator struct {
	min, max, cur config.DayTime
	curPeriod     config.PeriodConfig
	schemaCfg     config.SchemaConfig
	err           error
}

func newDayRangeIterator(minVal, maxVal config.DayTime, schemaCfg config.SchemaConfig) *dayRangeIterator {
	return &dayRangeIterator{min: minVal, max: maxVal, cur: minVal.Dec(), schemaCfg: schemaCfg}
}

func (r *dayRangeIterator) TotalDays() int {
	offset := r.cur
	if r.cur.Before(r.min) {
		offset = r.min
	}
	return int(r.max.Sub(offset.Time) / config.ObjectStorageIndexRequiredPeriod)
}

func (r *dayRangeIterator) Next() bool {
	r.cur = r.cur.Inc()
	if !r.cur.Before(r.max) {
		return false
	}

	period, err := r.schemaCfg.SchemaForTime(r.cur.ModelTime())
	if err != nil {
		r.err = fmt.Errorf("getting schema for time (%s): %w", r.cur, err)
		return false
	}
	r.curPeriod = period

	return true
}

func (r *dayRangeIterator) At() config.DayTable {
	return config.NewDayTable(r.cur, r.curPeriod.IndexTables.Prefix)
}

func (r *dayRangeIterator) Err() error {
	return nil
}
