package ingester

type Config struct {
}

type Ingester struct {
	cfg Config
}

func New(cfg Config) (*Ingester, error) {
	return &Ingester{
		cfg: cfg,
	}, nil
}
