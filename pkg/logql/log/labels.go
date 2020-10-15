package log

// Labels is the type that is passed across multiple stages.
// I expect this type to become more and more complex over time as we optimize it.
type Labels map[string]string

func (l Labels) Has(key string) bool {
	_, ok := l[key]
	return ok
}

func (l Labels) SetError(err string) {
	l[ErrorLabel] = err
}

func (l Labels) HasError() bool {
	_, ok := l[ErrorLabel]
	return ok
}
