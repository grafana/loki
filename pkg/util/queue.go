package util

type Queue interface {
	Append(entry any)
	Entries() []any
	Length() int
	Clear()
}
