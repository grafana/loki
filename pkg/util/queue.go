package util //nolint:revive

type Queue interface {
	Append(entry any)
	Entries() []any
	Length() int
	Clear()
}
