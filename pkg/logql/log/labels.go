package log

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
