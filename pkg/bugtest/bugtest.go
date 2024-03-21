package bugtest

import (
	"encoding/json"

	"github.com/gogo/protobuf/types"
	"github.com/grafana/loki/pkg/storage/chunk/cache/resultscache"
)

type Resp struct {
	Start int64
	End   int64
	Resp  interface{}
}

func RespFromExtent(ext []resultscache.Extent) []Resp {
	res := make([]Resp, 0)
	for _, v := range ext {
		val, err := types.EmptyAny(v.Response)
		if err != nil {
			panic(err)
		}
		err = types.UnmarshalAny(v.Response, val)
		if err != nil {
			panic(err)
		}
		res = append(res, Resp{
			Start: v.Start,
			End:   v.End,
			Resp:  val,
		})
	}
	return res
}

func ToRespJSON(ext []resultscache.Extent) string {
	r := RespFromExtent(ext)
	v, err := json.Marshal(r)
	if err != nil {
		panic(err)
	}
	return string(v)
}
