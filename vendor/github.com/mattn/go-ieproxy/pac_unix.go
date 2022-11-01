//go:build (!windows && !darwin) || !cgo
// +build !windows,!darwin !cgo

package ieproxy

func (psc *ProxyScriptConf) findProxyForURL(URL string) string {
	return ""
}
