package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/pkg/errors"
)

// LogpushJob describes a Logpush job.
type LogpushJob struct {
	ID                 int        `json:"id,omitempty"`
	Dataset            string     `json:"dataset"`
	Enabled            bool       `json:"enabled"`
	Name               string     `json:"name"`
	LogpullOptions     string     `json:"logpull_options"`
	DestinationConf    string     `json:"destination_conf"`
	OwnershipChallenge string     `json:"ownership_challenge,omitempty"`
	LastComplete       *time.Time `json:"last_complete,omitempty"`
	LastError          *time.Time `json:"last_error,omitempty"`
	ErrorMessage       string     `json:"error_message,omitempty"`
}

// LogpushJobsResponse is the API response, containing an array of Logpush Jobs.
type LogpushJobsResponse struct {
	Response
	Result []LogpushJob `json:"result"`
}

// LogpushJobDetailsResponse is the API response, containing a single Logpush Job.
type LogpushJobDetailsResponse struct {
	Response
	Result LogpushJob `json:"result"`
}

// LogpushFieldsResponse is the API response for a datasets fields
type LogpushFieldsResponse struct {
	Response
	Result LogpushFields `json:"result"`
}

// LogpushFields is a map of available Logpush field names & descriptions
type LogpushFields map[string]string

// LogpushGetOwnershipChallenge describes a ownership validation.
type LogpushGetOwnershipChallenge struct {
	Filename string `json:"filename"`
	Valid    bool   `json:"valid"`
	Message  string `json:"message"`
}

// LogpushGetOwnershipChallengeResponse is the API response, containing a ownership challenge.
type LogpushGetOwnershipChallengeResponse struct {
	Response
	Result LogpushGetOwnershipChallenge `json:"result"`
}

// LogpushGetOwnershipChallengeRequest is the API request for get ownership challenge.
type LogpushGetOwnershipChallengeRequest struct {
	DestinationConf string `json:"destination_conf"`
}

// LogpushOwnershipChallengeValidationResponse is the API response,
// containing a ownership challenge validation result.
type LogpushOwnershipChallengeValidationResponse struct {
	Response
	Result struct {
		Valid bool `json:"valid"`
	}
}

// LogpushValidateOwnershipChallengeRequest is the API request for validate ownership challenge.
type LogpushValidateOwnershipChallengeRequest struct {
	DestinationConf    string `json:"destination_conf"`
	OwnershipChallenge string `json:"ownership_challenge"`
}

// LogpushDestinationExistsResponse is the API response,
// containing a destination exists check result.
type LogpushDestinationExistsResponse struct {
	Response
	Result struct {
		Exists bool `json:"exists"`
	}
}

// LogpushDestinationExistsRequest is the API request for check destination exists.
type LogpushDestinationExistsRequest struct {
	DestinationConf string `json:"destination_conf"`
}

// CreateAccountLogpushJob creates a new account-level Logpush Job.
//
// API reference: https://api.cloudflare.com/#logpush-jobs-create-logpush-job
func (api *API) CreateAccountLogpushJob(ctx context.Context, accountID string, job LogpushJob) (*LogpushJob, error) {
	return api.createLogpushJob(ctx, AccountRouteRoot, accountID, job)
}

// CreateZoneLogpushJob creates a new zone-level Logpush Job.
//
// API reference: https://api.cloudflare.com/#logpush-jobs-create-logpush-job
func (api *API) CreateZoneLogpushJob(ctx context.Context, zoneID string, job LogpushJob) (*LogpushJob, error) {
	return api.createLogpushJob(ctx, ZoneRouteRoot, zoneID, job)
}

// CreateLogpushJob creates a new zone-level Logpush Job.
//
// API reference: https://api.cloudflare.com/#logpush-jobs-create-logpush-job
//
// Deprecated: Use `CreateZoneLogpushJob` or `CreateAccountLogpushJob` depending 
// on the desired resource to target.
func (api *API) CreateLogpushJob(ctx context.Context, zoneID string, job LogpushJob) (*LogpushJob, error) {
	return api.createLogpushJob(ctx, ZoneRouteRoot, zoneID, job)
}

func (api *API) createLogpushJob(ctx context.Context, identifierType RouteRoot, identifier string, job LogpushJob) (*LogpushJob, error) {
	uri := fmt.Sprintf("/%s/%s/logpush/jobs", identifierType, identifier)
	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, job)
	if err != nil {
		return nil, err
	}
	var r LogpushJobDetailsResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}
	return &r.Result, nil
}

// ListAccountLogpushJobs returns all account-level Logpush Jobs for all datasets.
//
// API reference: https://api.cloudflare.com/#logpush-jobs-list-logpush-jobs
func (api *API) ListAccountLogpushJobs(ctx context.Context, accountID string) ([]LogpushJob, error) {
	return api.listLogpushJobs(ctx, AccountRouteRoot, accountID)
}

// ListZoneLogpushJobs returns all zone-level Logpush Jobs for all datasets.
//
// API reference: https://api.cloudflare.com/#logpush-jobs-list-logpush-jobs
func (api *API) ListZoneLogpushJobs(ctx context.Context, zoneID string) ([]LogpushJob, error) {
	return api.listLogpushJobs(ctx, ZoneRouteRoot, zoneID)
}

// LogpushJobs returns all zone-level Logpush Jobs for all datasets.
//
// API reference: https://api.cloudflare.com/#logpush-jobs-list-logpush-jobs
//
// Deprecated: Use `ListZoneLogpushJobs` or `ListAccountLogpushJobs`
// depending on the desired resource to target.
func (api *API) LogpushJobs(ctx context.Context, zoneID string) ([]LogpushJob, error) {
	return api.listLogpushJobs(ctx, ZoneRouteRoot, zoneID)
}

func (api *API) listLogpushJobs(ctx context.Context, identifierType RouteRoot, identifier string) ([]LogpushJob, error) {
	uri := fmt.Sprintf("/%s/%s/logpush/jobs", identifierType, identifier)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return []LogpushJob{}, err
	}
	var r LogpushJobsResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return []LogpushJob{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// ListAccountLogpushJobsForDataset returns all account-level Logpush Jobs for a dataset.
//
// API reference: https://api.cloudflare.com/#logpush-jobs-list-logpush-jobs-for-a-dataset
func (api *API) ListAccountLogpushJobsForDataset(ctx context.Context, accountID, dataset string) ([]LogpushJob, error) {
	return api.listLogpushJobsForDataset(ctx, AccountRouteRoot, accountID, dataset)
}

// ListZoneLogpushJobsForDataset returns all zone-level Logpush Jobs for a dataset.
//
// API reference: https://api.cloudflare.com/#logpush-jobs-list-logpush-jobs-for-a-dataset
func (api *API) ListZoneLogpushJobsForDataset(ctx context.Context, zoneID, dataset string) ([]LogpushJob, error) {
	return api.listLogpushJobsForDataset(ctx, ZoneRouteRoot, zoneID, dataset)
}

// LogpushJobsForDataset returns all zone-level Logpush Jobs for a dataset.
//
// API reference: https://api.cloudflare.com/#logpush-jobs-list-logpush-jobs-for-a-dataset
//
// Deprecated: Use `ListZoneLogpushJobsForDataset` or 
// `ListAccountLogpushJobsForDataset` depending on the desired resource
// to target.
func (api *API) LogpushJobsForDataset(ctx context.Context, zoneID, dataset string) ([]LogpushJob, error) {
	return api.listLogpushJobsForDataset(ctx, ZoneRouteRoot, zoneID, dataset)
}

func (api *API) listLogpushJobsForDataset(ctx context.Context, identifierType RouteRoot, identifier, dataset string) ([]LogpushJob, error) {
	uri := fmt.Sprintf("/%s/%s/logpush/datasets/%s/jobs", identifierType, identifier, dataset)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return []LogpushJob{}, err
	}
	var r LogpushJobsResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return []LogpushJob{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// GetAccountLogpushFields returns fields for a given account-level dataset.
//
// Account fields documentation: https://developers.cloudflare.com/logs/reference/log-fields/account
//
// API reference: https://api.cloudflare.com/#logpush-jobs-list-logpush-jobs
func (api *API) GetAccountLogpushFields(ctx context.Context, accountID, dataset string) (LogpushFields, error) {
	return api.getLogpushFields(ctx, AccountRouteRoot, accountID, dataset)
}

// GetZoneLogpushFields returns fields for a given zone-level dataset.
//
// Zone fields documentation: https://developers.cloudflare.com/logs/reference/log-fields/zone
//
// API reference: https://api.cloudflare.com/#logpush-jobs-list-logpush-jobs
func (api *API) GetZoneLogpushFields(ctx context.Context, zoneID, dataset string) (LogpushFields, error) {
	return api.getLogpushFields(ctx, ZoneRouteRoot, zoneID, dataset)
}

// LogpushFields returns fields for a given dataset.
//
// API reference: https://api.cloudflare.com/#logpush-jobs-list-logpush-jobs
//
// Deprecated: Use `GetZoneLogpushFields` or `GetAccountLogpushFields` 
// depending on the desired resource to target.
func (api *API) LogpushFields(ctx context.Context, zoneID, dataset string) (LogpushFields, error) {
	return api.getLogpushFields(ctx, ZoneRouteRoot, zoneID, dataset)
}

func (api *API) getLogpushFields(ctx context.Context, identifierType RouteRoot, identifier, dataset string) (LogpushFields, error) {
	uri := fmt.Sprintf("/%s/%s/logpush/datasets/%s/fields", identifierType, identifier, dataset)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return LogpushFields{}, err
	}
	var r LogpushFieldsResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return LogpushFields{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// GetAccountLogpushJob fetches detail about one account-level Logpush Job.
//
// API reference: https://api.cloudflare.com/#logpush-jobs-logpush-job-details
func (api *API) GetAccountLogpushJob(ctx context.Context, accountID string, jobID int) (LogpushJob, error) {
	return api.getLogpushJob(ctx, AccountRouteRoot, accountID, jobID)
}

// GetZoneLogpushJob fetches detail about one Logpush Job for a zone.
//
// API reference: https://api.cloudflare.com/#logpush-jobs-logpush-job-details
func (api *API) GetZoneLogpushJob(ctx context.Context, zoneID string, jobID int) (LogpushJob, error) {
	return api.getLogpushJob(ctx, ZoneRouteRoot, zoneID, jobID)
}

// LogpushJob fetches detail about one Logpush Job for a zone.
//
// API reference: https://api.cloudflare.com/#logpush-jobs-logpush-job-details
//
// Deprecated: Use `GetZoneLogpushJob` or `GetAccountLogpushJob` 
// depending on the desired resource to target.
func (api *API) LogpushJob(ctx context.Context, zoneID string, jobID int) (LogpushJob, error) {
	return api.getLogpushJob(ctx, ZoneRouteRoot, zoneID, jobID)
}

func (api *API) getLogpushJob(ctx context.Context, identifierType RouteRoot, identifier string, jobID int) (LogpushJob, error) {
	uri := fmt.Sprintf("/%s/%s/logpush/jobs/%d", identifierType, identifier, jobID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return LogpushJob{}, err
	}
	var r LogpushJobDetailsResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return LogpushJob{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// UpdateAccountLogpushJob lets you update an account-level Logpush Job.
//
// API reference: https://api.cloudflare.com/#logpush-jobs-update-logpush-job
func (api *API) UpdateAccountLogpushJob(ctx context.Context, accountID string, jobID int, job LogpushJob) error {
	return api.updateLogpushJob(ctx, AccountRouteRoot, accountID, jobID, job)
}

// UpdateZoneLogpushJob lets you update a Logpush Job for a zone.
//
// API reference: https://api.cloudflare.com/#logpush-jobs-update-logpush-job
func (api *API) UpdateZoneLogpushJob(ctx context.Context, zoneID string, jobID int, job LogpushJob) error {
	return api.updateLogpushJob(ctx, ZoneRouteRoot, zoneID, jobID, job)
}

// UpdateLogpushJob lets you update a Logpush Job.
//
// API reference: https://api.cloudflare.com/#logpush-jobs-update-logpush-job
//
// Deprecated: Use `UpdateZoneLogpushJob` or `UpdateAccountLogpushJob` 
// depending on the desired resource to target.
func (api *API) UpdateLogpushJob(ctx context.Context, zoneID string, jobID int, job LogpushJob) error {
	return api.updateLogpushJob(ctx, ZoneRouteRoot, zoneID, jobID, job)
}

func (api *API) updateLogpushJob(ctx context.Context, identifierType RouteRoot, identifier string, jobID int, job LogpushJob) error {
	uri := fmt.Sprintf("/%s/%s/logpush/jobs/%d", identifierType, identifier, jobID)
	res, err := api.makeRequestContext(ctx, http.MethodPut, uri, job)
	if err != nil {
		return err
	}
	var r LogpushJobDetailsResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return errors.Wrap(err, errUnmarshalError)
	}
	return nil
}

// DeleteAccountLogpushJob deletes an account-level Logpush Job.
//
// API reference: https://api.cloudflare.com/#logpush-jobs-delete-logpush-job
func (api *API) DeleteAccountLogpushJob(ctx context.Context, accountID string, jobID int) error {
	return api.deleteLogpushJob(ctx, AccountRouteRoot, accountID, jobID)
}

// DeleteZoneLogpushJob deletes a Logpush Job for a zone.
//
// API reference: https://api.cloudflare.com/#logpush-jobs-delete-logpush-job
func (api *API) DeleteZoneLogpushJob(ctx context.Context, zoneID string, jobID int) error {
	return api.deleteLogpushJob(ctx, ZoneRouteRoot, zoneID, jobID)
}

// DeleteLogpushJob deletes a Logpush Job for a zone.
//
// API reference: https://api.cloudflare.com/#logpush-jobs-delete-logpush-job
//
// Deprecated: Use `DeleteZoneLogpushJob` or `DeleteAccountLogpushJob` 
// depending on the desired resource to target.
func (api *API) DeleteLogpushJob(ctx context.Context, zoneID string, jobID int) error {
	return api.deleteLogpushJob(ctx, ZoneRouteRoot, zoneID, jobID)
}

func (api *API) deleteLogpushJob(ctx context.Context, identifierType RouteRoot, identifier string, jobID int) error {
	uri := fmt.Sprintf("/%s/%s/logpush/jobs/%d", identifierType, identifier, jobID)
	res, err := api.makeRequestContext(ctx, http.MethodDelete, uri, nil)
	if err != nil {
		return err
	}
	var r LogpushJobDetailsResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return errors.Wrap(err, errUnmarshalError)
	}
	return nil
}

// GetAccountLogpushOwnershipChallenge returns ownership challenge.
//
// API reference: https://api.cloudflare.com/#logpush-jobs-get-ownership-challenge
func (api *API) GetAccountLogpushOwnershipChallenge(ctx context.Context, accountID, destinationConf string) (*LogpushGetOwnershipChallenge, error) {
	return api.getLogpushOwnershipChallenge(ctx, AccountRouteRoot, accountID, destinationConf)
}

// GetZoneLogpushOwnershipChallenge returns ownership challenge.
//
// API reference: https://api.cloudflare.com/#logpush-jobs-get-ownership-challenge
func (api *API) GetZoneLogpushOwnershipChallenge(ctx context.Context, zoneID, destinationConf string) (*LogpushGetOwnershipChallenge, error) {
	return api.getLogpushOwnershipChallenge(ctx, ZoneRouteRoot, zoneID, destinationConf)
}

// GetLogpushOwnershipChallenge returns ownership challenge.
//
// API reference: https://api.cloudflare.com/#logpush-jobs-get-ownership-challenge
//
// Deprecated: Use `GetZoneLogpushOwnershipChallenge` or
// `GetAccountLogpushOwnershipChallenge` depending on the 
// desired resource to target.
func (api *API) GetLogpushOwnershipChallenge(ctx context.Context, zoneID, destinationConf string) (*LogpushGetOwnershipChallenge, error) {
	return api.getLogpushOwnershipChallenge(ctx, ZoneRouteRoot, zoneID, destinationConf)
}

func (api *API) getLogpushOwnershipChallenge(ctx context.Context, identifierType RouteRoot, identifier, destinationConf string) (*LogpushGetOwnershipChallenge, error) {
	uri := fmt.Sprintf("/%s/%s/logpush/ownership", identifierType, identifier)
	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, LogpushGetOwnershipChallengeRequest{
		DestinationConf: destinationConf,
	})
	if err != nil {
		return nil, err
	}
	var r LogpushGetOwnershipChallengeResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}

	if !r.Result.Valid {
		return nil, errors.New(r.Result.Message)
	}

	return &r.Result, nil
}

// ValidateAccountLogpushOwnershipChallenge returns account-level ownership challenge validation result.
//
// API reference: https://api.cloudflare.com/#logpush-jobs-validate-ownership-challenge
func (api *API) ValidateAccountLogpushOwnershipChallenge(ctx context.Context, accountID, destinationConf, ownershipChallenge string) (bool, error) {
	return api.validateLogpushOwnershipChallenge(ctx, AccountRouteRoot, accountID, destinationConf, ownershipChallenge)
}

// ValidateZoneLogpushOwnershipChallenge returns zone-level ownership challenge validation result.
//
// API reference: https://api.cloudflare.com/#logpush-jobs-validate-ownership-challenge
func (api *API) ValidateZoneLogpushOwnershipChallenge(ctx context.Context, zoneID, destinationConf, ownershipChallenge string) (bool, error) {
	return api.validateLogpushOwnershipChallenge(ctx, ZoneRouteRoot, zoneID, destinationConf, ownershipChallenge)
}

// ValidateLogpushOwnershipChallenge returns zone-level ownership challenge validation result.
//
// API reference: https://api.cloudflare.com/#logpush-jobs-validate-ownership-challenge
//
// Deprecated: Use `ValidateZoneLogpushOwnershipChallenge` or
// `ValidateAccountLogpushOwnershipChallenge` depending on the 
// desired resource to target.
func (api *API) ValidateLogpushOwnershipChallenge(ctx context.Context, zoneID, destinationConf, ownershipChallenge string) (bool, error) {
	return api.validateLogpushOwnershipChallenge(ctx, ZoneRouteRoot, zoneID, destinationConf, ownershipChallenge)
}

func (api *API) validateLogpushOwnershipChallenge(ctx context.Context, identifierType RouteRoot, identifier, destinationConf, ownershipChallenge string) (bool, error) {
	uri := fmt.Sprintf("/%s/%s/logpush/ownership/validate", identifierType, identifier)
	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, LogpushValidateOwnershipChallengeRequest{
		DestinationConf:    destinationConf,
		OwnershipChallenge: ownershipChallenge,
	})
	if err != nil {
		return false, err
	}
	var r LogpushGetOwnershipChallengeResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return false, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result.Valid, nil
}

// CheckAccountLogpushDestinationExists returns account-level destination exists check result.
//
// API reference: https://api.cloudflare.com/#logpush-jobs-check-destination-exists
func (api *API) CheckAccountLogpushDestinationExists(ctx context.Context, accountID, destinationConf string) (bool, error) {
	return api.checkLogpushDestinationExists(ctx, AccountRouteRoot, accountID, destinationConf)
}

// CheckZoneLogpushDestinationExists returns zone-level destination exists check result.
//
// API reference: https://api.cloudflare.com/#logpush-jobs-check-destination-exists
func (api *API) CheckZoneLogpushDestinationExists(ctx context.Context, zoneID, destinationConf string) (bool, error) {
	return api.checkLogpushDestinationExists(ctx, ZoneRouteRoot, zoneID, destinationConf)
}

// CheckLogpushDestinationExists returns zone-level destination exists check result.
//
// API reference: https://api.cloudflare.com/#logpush-jobs-check-destination-exists
//
// Deprecated: Use `CheckZoneLogpushDestinationExists` or
// `CheckAccountLogpushDestinationExists` depending 
// on the desired resource to target.
func (api *API) CheckLogpushDestinationExists(ctx context.Context, zoneID, destinationConf string) (bool, error) {
	return api.checkLogpushDestinationExists(ctx, ZoneRouteRoot, zoneID, destinationConf)
}

func (api *API) checkLogpushDestinationExists(ctx context.Context, identifierType RouteRoot, identifier, destinationConf string) (bool, error) {
	uri := fmt.Sprintf("/%s/%s/logpush/validate/destination/exists", identifierType, identifier)
	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, LogpushDestinationExistsRequest{
		DestinationConf: destinationConf,
	})
	if err != nil {
		return false, err
	}
	var r LogpushDestinationExistsResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return false, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result.Exists, nil
}
