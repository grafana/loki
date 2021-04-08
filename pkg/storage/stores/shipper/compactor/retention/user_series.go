package retention

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

type userSeriesMap map[string]userSeries

func newUserSeriesMap() userSeriesMap {
	return make(userSeriesMap)
}

func (u userSeriesMap) Add(seriesID []byte, userID []byte) {
	us := newUserSeries(seriesID, userID)
	u[us.Key()] = us
}

func (u userSeriesMap) ForEach(callback func(seriesID []byte, userID []byte) error) error {
	for _, v := range u {
		if err := callback(v.SeriesID(), v.UserID()); err != nil {
			return err
		}
	}
	return nil
}
