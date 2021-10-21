package retention

import (
	"github.com/prometheus/prometheus/pkg/labels"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/storage/chunk"
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

type userSeriesInfo struct {
	userSeries
	isDeleted bool
	lbls      labels.Labels
}

type userSeriesMap map[string]userSeriesInfo

func newUserSeriesMap() userSeriesMap {
	return make(userSeriesMap)
}

func (u userSeriesMap) Add(seriesID []byte, userID []byte, lbls labels.Labels) {
	us := newUserSeries(seriesID, userID)
	if _, ok := u[us.Key()]; ok {
		return
	}

	u[us.Key()] = userSeriesInfo{
		userSeries: us,
		isDeleted:  true,
		lbls:       lbls,
	}
}

// MarkSeriesNotDeleted is used to mark series not deleted when it still has some chunks left in the store
func (u userSeriesMap) MarkSeriesNotDeleted(seriesID []byte, userID []byte) {
	us := newUserSeries(seriesID, userID)
	usi := u[us.Key()]
	usi.isDeleted = false
	u[us.Key()] = usi
}

func (u userSeriesMap) ForEach(callback func(info userSeriesInfo) error) error {
	for _, v := range u {
		if err := callback(v); err != nil {
			return err
		}
	}
	return nil
}

type seriesLabels struct {
	userSeries
	lbs labels.Labels
}

type seriesLabelsMapper struct {
	cursor *bbolt.Cursor
	config chunk.PeriodConfig

	bufKey  userSeries
	mapping map[string]*seriesLabels
}

func newSeriesLabelsMapper(bucket *bbolt.Bucket, config chunk.PeriodConfig) (*seriesLabelsMapper, error) {
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
