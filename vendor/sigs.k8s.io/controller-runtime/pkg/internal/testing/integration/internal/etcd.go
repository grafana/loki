package internal

import (
	"net/url"
)

// EtcdDefaultArgs allow tests to run offline, by preventing API server from attempting to
// use default route to determine its urls.
var EtcdDefaultArgs = []string{
	"--listen-peer-urls=http://localhost:0",
	"--advertise-client-urls={{ if .URL }}{{ .URL.String }}{{ end }}",
	"--listen-client-urls={{ if .URL }}{{ .URL.String }}{{ end }}",
	"--data-dir={{ .DataDir }}",
}

// DoEtcdArgDefaulting will set default values to allow tests to run offline when the args are not informed. Otherwise,
// it will return the same []string arg passed as param.
func DoEtcdArgDefaulting(args []string) []string {
	if len(args) != 0 {
		return args
	}

	return EtcdDefaultArgs
}

// isSecureScheme returns false when the schema is insecure.
func isSecureScheme(scheme string) bool {
	// https://github.com/coreos/etcd/blob/d9deeff49a080a88c982d328ad9d33f26d1ad7b6/pkg/transport/listener.go#L53
	if scheme == "https" || scheme == "unixs" {
		return true
	}
	return false
}

// GetEtcdStartMessage returns an start message to inform if the client is or not insecure.
// It will return true when the URL informed has the scheme == "https" || scheme == "unixs"
func GetEtcdStartMessage(listenURL url.URL) string {
	if isSecureScheme(listenURL.Scheme) {
		// https://github.com/coreos/etcd/blob/a7f1fbe00ec216fcb3a1919397a103b41dca8413/embed/serve.go#L167
		return "serving client requests on "
	}

	// https://github.com/coreos/etcd/blob/a7f1fbe00ec216fcb3a1919397a103b41dca8413/embed/serve.go#L124
	return "serving insecure client requests on "
}
