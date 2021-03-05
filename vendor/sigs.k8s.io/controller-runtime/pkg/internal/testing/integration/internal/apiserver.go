package internal

// APIServerDefaultArgs allow tests to run offline, by preventing API server from attempting to
// use default route to determine its --advertise-address.
var APIServerDefaultArgs = []string{
	"--advertise-address=127.0.0.1",
	"--etcd-servers={{ if .EtcdURL }}{{ .EtcdURL.String }}{{ end }}",
	"--cert-dir={{ .CertDir }}",
	"--insecure-port={{ if .URL }}{{ .URL.Port }}{{ end }}",
	"--insecure-bind-address={{ if .URL }}{{ .URL.Hostname }}{{ end }}",
	"--secure-port={{ if .SecurePort }}{{ .SecurePort }}{{ end }}",
	// we're keeping this disabled because if enabled, default SA is missing which would force all tests to create one
	// in normal apiserver operation this SA is created by controller, but that is not run in integration environment
	"--disable-admission-plugins=ServiceAccount",
	"--service-cluster-ip-range=10.0.0.0/24",
	"--allow-privileged=true",
}

// DoAPIServerArgDefaulting will set default values to allow tests to run offline when the args are not informed. Otherwise,
// it will return the same []string arg passed as param.
func DoAPIServerArgDefaulting(args []string) []string {
	if len(args) != 0 {
		return args
	}

	return APIServerDefaultArgs
}
