package kfake

type tps[V any] map[string]map[int32]*V

func (tps *tps[V]) getp(t string, p int32) (*V, bool) {
	if *tps == nil {
		return nil, false
	}
	ps := (*tps)[t]
	if ps == nil {
		return nil, false
	}
	v, ok := ps[p]
	return v, ok
}

func (tps *tps[V]) gett(t string) (map[int32]*V, bool) {
	if tps == nil {
		return nil, false
	}
	ps, ok := (*tps)[t]
	return ps, ok
}

func (tps *tps[V]) mkt(t string) map[int32]*V {
	if *tps == nil {
		*tps = make(map[string]map[int32]*V)
	}
	ps := (*tps)[t]
	if ps == nil {
		ps = make(map[int32]*V)
		(*tps)[t] = ps
	}
	return ps
}

func (tps *tps[V]) mkp(t string, p int32, newFn func() *V) *V {
	ps := tps.mkt(t)
	v, ok := ps[p]
	if !ok {
		v = newFn()
		ps[p] = v
	}
	return v
}

func (tps *tps[V]) mkpDefault(t string, p int32) *V {
	return tps.mkp(t, p, func() *V { return new(V) })
}

func (tps *tps[V]) set(t string, p int32, v V) {
	*tps.mkpDefault(t, p) = v
}

func (tps *tps[V]) each(fn func(t string, p int32, v *V)) {
	for t, ps := range *tps {
		for p, v := range ps {
			fn(t, p, v)
		}
	}
}

func (tps *tps[V]) delp(t string, p int32) {
	if *tps == nil {
		return
	}
	ps := (*tps)[t]
	if ps == nil {
		return
	}
	delete(ps, p)
	if len(ps) == 0 {
		delete(*tps, t)
	}
}
