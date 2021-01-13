package swift

import (
	"flag"
)

// Config holds the config options for Swift backend
type Config struct {
	AuthURL           string `yaml:"auth_url"`
	Username          string `yaml:"username"`
	UserDomainName    string `yaml:"user_domain_name"`
	UserDomainID      string `yaml:"user_domain_id"`
	UserID            string `yaml:"user_id"`
	Password          string `yaml:"password"`
	DomainID          string `yaml:"domain_id"`
	DomainName        string `yaml:"domain_name"`
	ProjectID         string `yaml:"project_id"`
	ProjectName       string `yaml:"project_name"`
	ProjectDomainID   string `yaml:"project_domain_id"`
	ProjectDomainName string `yaml:"project_domain_name"`
	RegionName        string `yaml:"region_name"`
	ContainerName     string `yaml:"container_name"`
}

// RegisterFlags registers the flags for Swift storage
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix registers the flags for Swift storage with the provided prefix
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.AuthURL, prefix+"swift.auth-url", "", "OpenStack Swift authentication URL")
	f.StringVar(&cfg.Username, prefix+"swift.username", "", "OpenStack Swift username.")
	f.StringVar(&cfg.UserDomainName, prefix+"swift.user-domain-name", "", "OpenStack Swift user's domain name.")
	f.StringVar(&cfg.UserDomainID, prefix+"swift.user-domain-id", "", "OpenStack Swift user's domain ID.")
	f.StringVar(&cfg.UserID, prefix+"swift.user-id", "", "OpenStack Swift user ID.")
	f.StringVar(&cfg.Password, prefix+"swift.password", "", "OpenStack Swift API key.")
	f.StringVar(&cfg.DomainID, prefix+"swift.domain-id", "", "OpenStack Swift user's domain ID.")
	f.StringVar(&cfg.DomainName, prefix+"swift.domain-name", "", "OpenStack Swift user's domain name.")
	f.StringVar(&cfg.ProjectID, prefix+"swift.project-id", "", "OpenStack Swift project ID (v2,v3 auth only).")
	f.StringVar(&cfg.ProjectName, prefix+"swift.project-name", "", "OpenStack Swift project name (v2,v3 auth only).")
	f.StringVar(&cfg.ProjectDomainID, prefix+"swift.project-domain-id", "", "ID of the OpenStack Swift project's domain (v3 auth only), only needed if it differs the from user domain.")
	f.StringVar(&cfg.ProjectDomainName, prefix+"swift.project-domain-name", "", "Name of the OpenStack Swift project's domain (v3 auth only), only needed if it differs from the user domain.")
	f.StringVar(&cfg.RegionName, prefix+"swift.region-name", "", "OpenStack Swift Region to use (v2,v3 auth only).")
	f.StringVar(&cfg.ContainerName, prefix+"swift.container-name", "", "Name of the OpenStack Swift container to put chunks in.")
}
