package compactor

import (
	"github.com/prometheus/prometheus/model/labels"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/v3/pkg/storage/config"
)

type userSeries struct {
	key         []byte
	seriesIDLen int
}

func newUserSeries(seriesID []byte, userID []byte) userSeries {
	key := make([]byte, 0, len(seriesID)+len(userID))
	key = append(key, seriesID...)
	key = append(key, userID...)
	return userSeries{
		key:         key,
		seriesIDLen: len(seriesID),
	}
}

func (us userSeries) Key() string {
	return unsafeGetString(us.key)
}

func (us userSeries) SeriesID() []byte {
	return us.key[:us.seriesIDLen]
}

func (us userSeries) UserID() []byte {
	return us.key[us.seriesIDLen:]
}

func (us *userSeries) Reset(seriesID []byte, userID []byte) {
	if us.key == nil {
		us.key = make([]byte, 0, len(seriesID)+len(userID))
	}
	us.key = us.key[:0]
	us.key = append(us.key, seriesID...)
	us.key = append(us.key, userID...)
	us.seriesIDLen = len(seriesID)
}

type seriesLabels struct {
	userSeries
	lbs labels.Labels
}

type seriesLabelsMapper struct {
	cursor *bbolt.Cursor
	config config.PeriodConfig

	bufKey  userSeries
	mapping map[string]*seriesLabels
}

func newSeriesLabelsMapper(bucket *bbolt.Bucket, config config.PeriodConfig) (*seriesLabelsMapper, error) {
	sm := &seriesLabelsMapper{
		cursor:  bucket.Cursor(),
		mapping: map[string]*seriesLabels{},
		config:  config,
		bufKey:  newUserSeries(nil, nil),
	}
	if err := sm.build(); err != nil {
		return nil, err
	}
	return sm, nil
}

func (sm *seriesLabelsMapper) Get(seriesID []byte, userID []byte) labels.Labels {
	sm.bufKey.Reset(seriesID, userID)
	lbs, ok := sm.mapping[sm.bufKey.Key()]
	if ok {
		return lbs.lbs
	}
	return labels.Labels{}
}

func (sm *seriesLabelsMapper) build() error {
Outer:
	for k, v := sm.cursor.First(); k != nil; k, v = sm.cursor.Next() {
		ref, ok, err := parseLabelSeriesRangeKey(decodeKey(k))
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		sm.bufKey.Reset(ref.SeriesID, ref.UserID)
		lbs, ok := sm.mapping[sm.bufKey.Key()]
		if !ok {
			k := newUserSeries(ref.SeriesID, ref.UserID)
			lbs = &seriesLabels{
				userSeries: k,
				lbs:        make(labels.Labels, 0, 15),
			}
			sm.mapping[k.Key()] = lbs
		}
		// add the labels if it doesn't exist.
		for _, l := range lbs.lbs {
			if l.Name == unsafeGetString(ref.Name) {
				continue Outer
			}
		}
		lbs.lbs = append(lbs.lbs, labels.Label{Name: string(ref.Name), Value: string(v)})
	}
	return nil
}
