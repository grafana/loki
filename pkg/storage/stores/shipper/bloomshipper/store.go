package bloomshipper

type Store interface {
}

type BloomStore struct {
	shipper Shipper
}

func NewBloomStore(s Shipper) (*BloomStore, error) {
	return &BloomStore{
		shipper: s,
	}, nil
}
