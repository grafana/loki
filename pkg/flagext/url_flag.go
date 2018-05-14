package flagext

import "net/url"

type URL struct {
	*url.URL
}

func (u *URL) String() string {
	if u.URL != nil {
		return u.URL.String()
	}
	return ""
}

func (u *URL) Set(value string) error {
	var err error
	u.URL, err = url.Parse(value)
	return err
}
