package retention

import (
	"github.com/prometheus/prometheus/model/labels"
)

type userSeries struct {
	key         []byte
	seriesIDLen int
}

func newUserSeries(seriesID, userID []byte) userSeries {
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

type userSeriesInfo struct {
	userSeries
	isDeleted bool
	lbls      labels.Labels
}

type userSeriesMap map[string]userSeriesInfo

func newUserSeriesMap() userSeriesMap {
	return make(userSeriesMap)
}

func (u userSeriesMap) Add(seriesID, userID []byte, lbls labels.Labels) {
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
func (u userSeriesMap) MarkSeriesNotDeleted(seriesID, userID []byte) {
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

func (u userSeriesMap) HasSeries(seriesID, userID []byte) bool {
	us := newUserSeries(seriesID, userID)
	_, ok := u[us.Key()]
	return ok
}
