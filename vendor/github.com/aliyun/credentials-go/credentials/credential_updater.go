package credentials

import (
	"net/http"
	"time"
)

const defaultInAdvanceScale = 0.95

var hookDo = func(fn func(req *http.Request) (*http.Response, error)) func(req *http.Request) (*http.Response, error) {
	return fn
}

type credentialUpdater struct {
	credentialExpiration int
	lastUpdateTimestamp  int64
	inAdvanceScale       float64
}

func (updater *credentialUpdater) needUpdateCredential() (result bool) {
	if updater.inAdvanceScale == 0 {
		updater.inAdvanceScale = defaultInAdvanceScale
	}
	return time.Now().Unix()-updater.lastUpdateTimestamp >= int64(float64(updater.credentialExpiration)*updater.inAdvanceScale)
}
