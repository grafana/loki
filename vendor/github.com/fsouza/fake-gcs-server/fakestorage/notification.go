package fakestorage

import (
	"encoding/json"
	"net/http"

	"github.com/fsouza/fake-gcs-server/internal/notification"
	"github.com/gorilla/mux"
)

func (s *Server) insertNotification(r *http.Request) jsonResponse {
	bucketName := unescapeMuxVars(mux.Vars(r))["bucketName"]

	if _, err := s.backend.GetBucket(bucketName); err != nil {
		return jsonResponse{status: http.StatusNotFound}
	}

	var cfg notification.NotificationConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		return jsonResponse{status: http.StatusBadRequest, errorMessage: err.Error()}
	}
	if cfg.Topic == "" {
		return jsonResponse{status: http.StatusBadRequest, errorMessage: "topic is required"}
	}

	created := s.notificationRegistry.Insert(bucketName, cfg)
	return jsonResponse{status: http.StatusCreated, data: created}
}

func (s *Server) getNotification(r *http.Request) jsonResponse {
	vars := unescapeMuxVars(mux.Vars(r))
	bucketName := vars["bucketName"]
	notificationID := vars["notificationId"]

	cfg, ok := s.notificationRegistry.Get(bucketName, notificationID)
	if !ok {
		return jsonResponse{status: http.StatusNotFound}
	}
	return jsonResponse{data: cfg}
}

func (s *Server) listNotifications(r *http.Request) jsonResponse {
	bucketName := unescapeMuxVars(mux.Vars(r))["bucketName"]

	if _, err := s.backend.GetBucket(bucketName); err != nil {
		return jsonResponse{status: http.StatusNotFound}
	}

	cfgs := s.notificationRegistry.List(bucketName)
	if cfgs == nil {
		cfgs = []notification.NotificationConfig{}
	}
	return jsonResponse{data: map[string]interface{}{"kind": "storage#notifications", "items": cfgs}}
}

func (s *Server) deleteNotification(r *http.Request) jsonResponse {
	vars := unescapeMuxVars(mux.Vars(r))
	bucketName := vars["bucketName"]
	notificationID := vars["notificationId"]

	if !s.notificationRegistry.Delete(bucketName, notificationID) {
		return jsonResponse{status: http.StatusNotFound}
	}
	return jsonResponse{status: http.StatusNoContent}
}
