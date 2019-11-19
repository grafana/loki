package cfg

import (
	"flag"
	"time"
)

// Data is a test Data structure
type Data struct {
	Verbose bool   `yaml:"verbose"`
	Server  Server `yaml:"server"`
	TLS     TLS    `yaml:"tls"`
}

type Server struct {
	Port    int           `yaml:"port"`
	Timeout time.Duration `yaml:"timeout"`
}

type TLS struct {
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
}

// RegisterFlags makes Data implement flagext.Registerer for using flags
func (d *Data) RegisterFlags(fs *flag.FlagSet) {
	fs.BoolVar(&d.Verbose, "verbose", false, "")
	fs.IntVar(&d.Server.Port, "server.port", 80, "")
	fs.DurationVar(&d.Server.Timeout, "server.timeout", 60*time.Second, "")

	fs.StringVar(&d.TLS.Cert, "tls.cert", "DEFAULTCERT", "")
	fs.StringVar(&d.TLS.Key, "tls.key", "DEFAULTKEY", "")
}
