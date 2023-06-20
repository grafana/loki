package fakestorage

import (
	"encoding/json"
	"net/http"
)

func (s *Server) updateServerConfig(r *http.Request) jsonResponse {
	var configOptions struct {
		ExternalUrl string `json:"externalUrl,omitempty"`
		PublicHost  string `json:"publicHost,omitempty"`
	}
	err := json.NewDecoder(r.Body).Decode(&configOptions)
	if err != nil {
		return jsonResponse{
			status:       http.StatusBadRequest,
			errorMessage: "Update server config payload can not be parsed.",
		}
	}

	if configOptions.ExternalUrl != "" {
		s.externalURL = configOptions.ExternalUrl
	}

	if configOptions.PublicHost != "" {
		s.publicHost = configOptions.PublicHost
	}

	return jsonResponse{status: http.StatusOK}
}
