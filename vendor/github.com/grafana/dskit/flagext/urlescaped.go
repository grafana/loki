package flagext

import "net/url"

// URLEscaped is a url.URL that can be used as a flag.
// URL value it contains will always be URL escaped and safe.
type URLEscaped struct {
	*url.URL
}

// String implements flag.Value
func (v URLEscaped) String() string {
	if v.URL == nil {
		return ""
	}
	return v.URL.String()
}

// Set implements flag.Value
// Set make sure given URL string is escaped.
func (v *URLEscaped) Set(s string) error {
	s = url.QueryEscape(s)

	u, err := url.Parse(s)
	if err != nil {
		return err
	}
	v.URL = u
	return nil
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (v *URLEscaped) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}

	// An empty string means no URL has been configured.
	if s == "" {
		v.URL = nil
		return nil
	}

	return v.Set(s)
}

// Marshalyaml Implements yaml.Marshaler.
func (v URLEscaped) MarshalYAML() (interface{}, error) {
	if v.URL == nil {
		return "", nil
	}

	// Mask out passwords when marshalling URLs back to YAML.
	u := *v.URL
	if u.User != nil {
		if _, set := u.User.Password(); set {
			u.User = url.UserPassword(u.User.Username(), "********")
		}
	}

	return u.String(), nil
}
