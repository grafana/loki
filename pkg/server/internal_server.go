package server

import (
	"flag"
	"time"

	serverww "github.com/grafana/dskit/server"
)

// Config extends weaveworks server config
type Config struct {
	serverww.Config `yaml:",inline"`
	Enable          bool `yaml:"enable"`
}

// RegisterFlags add internal server flags to flagset
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.Config.HTTPListenAddress, "internal-server.http-listen-address", "localhost", "HTTP internal server listen address.")
	f.StringVar(&cfg.Config.HTTPListenNetwork, "internal-server.http-listen-network", serverww.DefaultNetwork, "HTTP internal server listen network, default tcp")
	f.StringVar(&cfg.Config.HTTPTLSConfig.TLSCertPath, "internal-server.http-tls-cert-path", "", "HTTP internal server cert path.")
	f.StringVar(&cfg.Config.HTTPTLSConfig.TLSKeyPath, "internal-server.http-tls-key-path", "", "HTTP internal server key path.")
	f.StringVar(&cfg.Config.HTTPTLSConfig.ClientAuth, "internal-server.http-tls-client-auth", "", "HTTP TLS Client Auth type.")
	f.StringVar(&cfg.Config.HTTPTLSConfig.ClientCAs, "internal-server.http-tls-ca-path", "", "HTTP TLS Client CA path.")
	f.StringVar(&cfg.Config.CipherSuites, "internal-server.http-tls-cipher-suites", "", "HTTP TLS Cipher Suites.")
	f.StringVar(&cfg.Config.MinVersion, "internal-server.http-tls-min-version", "", "HTTP TLS Min Version.")
	f.IntVar(&cfg.Config.HTTPListenPort, "internal-server.http-listen-port", 3101, "HTTP internal server listen port.")
	f.IntVar(&cfg.Config.HTTPConnLimit, "internal-server.http-conn-limit", 0, "Maximum number of simultaneous http connections, <=0 to disable")
	f.DurationVar(&cfg.Config.ServerGracefulShutdownTimeout, "internal-server.graceful-shutdown-timeout", 30*time.Second, "Timeout for graceful shutdowns")
	f.DurationVar(&cfg.Config.HTTPServerReadTimeout, "internal-server.http-read-timeout", 30*time.Second, "Read timeout for HTTP server")
	f.DurationVar(&cfg.Config.HTTPServerWriteTimeout, "internal-server.http-write-timeout", 30*time.Second, "Write timeout for HTTP server")
	f.DurationVar(&cfg.Config.HTTPServerIdleTimeout, "internal-server.http-idle-timeout", 120*time.Second, "Idle timeout for HTTP server")
	f.BoolVar(&cfg.Enable, "internal-server.enable", false, "Disable the internal http server.")
}
