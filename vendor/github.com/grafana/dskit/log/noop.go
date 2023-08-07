// Provenance-includes-location: https://github.com/weaveworks/common/blob/main/logging/noop.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: Weaveworks Ltd.

package log

// Noop logger.
func Noop() Interface {
	return noop{}
}

type noop struct{}

func (noop) Debugf(string, ...interface{}) {}
func (noop) Debugln(...interface{})        {}
func (noop) Infof(string, ...interface{})  {}
func (noop) Infoln(...interface{})         {}
func (noop) Warnf(string, ...interface{})  {}
func (noop) Warnln(...interface{})         {}
func (noop) Errorf(string, ...interface{}) {}
func (noop) Errorln(...interface{})        {}
func (noop) WithField(string, interface{}) Interface {
	return noop{}
}
func (noop) WithFields(Fields) Interface {
	return noop{}
}
