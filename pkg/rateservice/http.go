package rateservice

import (
	"net/http"

	"github.com/gorilla/mux"

	"github.com/grafana/loki/v3/pkg/rateservice/proto"
	"github.com/grafana/loki/v3/pkg/util"
)

type httpGetRealmResponse struct {
	Realm   string              `json:"realm"`
	Results []*proto.RateResult `json:"results"`
}

func (s *Service) GetRealmHandler(w http.ResponseWriter, r *http.Request) {
	realm := mux.Vars(r)["realm"]
	if realm == "" {
		http.Error(w, "invalid realm", http.StatusBadRequest)
		return
	}
	res, ok := s.store.GetRealm(realm)
	if !ok {
		http.Error(w, "unknown realm", http.StatusNotFound)
		return
	}
	util.WriteJSONResponse(w, httpGetRealmResponse{
		Realm:   realm,
		Results: res,
	})
}
