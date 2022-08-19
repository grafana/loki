package plumbing

import (
	"errors"
	"fmt"
	"strings"
)

type ArgMap map[string]string

func (a *ArgMap) String() string {
	return fmt.Sprintf("%v", *a)
}

func (a *ArgMap) Set(val string) error {
	p := strings.Split(val, "=")

	if len(p) < 2 {
		return errors.New("invalid value")
	}

	var (
		k = p[0]
		v = p[1:]
	)

	(*a)[k] = strings.Join(v, "=")

	return nil
}

func (a *ArgMap) Get(key string) (string, error) {
	if v, ok := (*a)[key]; ok {
		return v, nil
	}

	return "", errors.New("value not found")
}
