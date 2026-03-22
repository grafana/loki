package loki

import (
	"fmt"
	"net/http"
)

func (t *Loki) servicesHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(200)
	w.Header().Set("Content-Type", "text/plain")

	// TODO: this could be extended to also print sub-services, if given service has any
	for mod, s := range t.serviceMap {
		if s != nil {
			fmt.Fprintf(w, "%v => %v\n", mod, s.State())
		}
	}
}
