package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/pkg/errors"
)

// PagesProject represents a Pages project.
type PagesProject struct {
	Name                string                        `json:"name"`
	ID                  string                        `json:"id"`
	CreatedOn           *time.Time                    `json:"created_on"`
	SubDomain           string                        `json:"subdomain"`
	Domains             []string                      `json:"domains,omitempty"`
	Source              PagesProjectSource            `json:"source"`
	BuildConfig         PagesProjectBuildConfig       `json:"build_config"`
	DeploymentConfigs   PagesProjectDeploymentConfigs `json:"deployment_configs"`
	LatestDeployment    PagesProjectDeployment        `json:"latest_deployment"`
	CanonicalDeployment PagesProjectDeployment        `json:"canonical_deployment"`
}

// PagesProjectSource represents the configuration of a Pages project source.
type PagesProjectSource struct {
	Type   string                    `json:"type"`
	Config *PagesProjectSourceConfig `json:"config"`
}

// PagesProjectSourceConfig represents the properties use to configure a Pages project source.
type PagesProjectSourceConfig struct {
	Owner              string `json:"owner"`
	RepoName           string `json:"repo_name"`
	ProductionBranch   string `json:"production_branch"`
	PRCommentsEnabled  bool   `json:"pr_comments_enabled"`
	DeploymentsEnabled bool   `json:"deployments_enabled"`
}

// PagesProjectBuildConfig represents the configuration of a Pages project build process.
type PagesProjectBuildConfig struct {
	BuildCommand      string `json:"build_command"`
	DestinationDir    string `json:"destination_dir"`
	RootDir           string `json:"root_dir"`
	WebAnalyticsTag   string `json:"web_analytics_tag"`
	WebAnalyticsToken string `json:"web_analytics_token"`
}

// PagesProjectDeploymentConfigs represents the configuration for deployments in a Pages project.
type PagesProjectDeploymentConfigs struct {
	Preview    PagesProjectDeploymentConfigEnvironment `json:"preview"`
	Production PagesProjectDeploymentConfigEnvironment `json:"production"`
}

// PagesProjectDeploymentConfigEnvironment represents the configuration for preview or production deploys.
type PagesProjectDeploymentConfigEnvironment struct {
	EnvVars PagesProjectDeploymentConfigEnvVars `json:"env_vars"`
}

// PagesProjectDeploymentConfigEnvVars represents the BUILD_VERSION environment variables for a specific build config.
type PagesProjectDeploymentConfigEnvVars struct {
	BuildVersion PagesProjectDeploymentConfigBuildVersion `json:"BUILD_VERSION"`
}

// PagesProjectDeploymentConfigBuildVersion represents a value for a BUILD_VERSION.
type PagesProjectDeploymentConfigBuildVersion struct {
	Value string `json:"value"`
}

// PagesProjectDeployment represents a deployment to a Pages project.
type PagesProjectDeployment struct {
	ID                string                        `json:"id"`
	ShortID           string                        `json:"short_id"`
	ProjectID         string                        `json:"project_id"`
	ProjectName       string                        `json:"project_name"`
	Environment       string                        `json:"environment"`
	URL               string                        `json:"url"`
	CreatedOn         *time.Time                    `json:"created_on"`
	ModifiedOn        *time.Time                    `json:"modified_on"`
	Aliases           []string                      `json:"aliases,omitempty"`
	LatestStage       PagesProjectDeploymentStage   `json:"latest_stage"`
	EnvVars           map[string]map[string]string  `json:"env_vars"`
	DeploymentTrigger PagesProjectDeploymentTrigger `json:"deployment_trigger"`
	Stages            []PagesProjectDeploymentStage `json:"stages"`
	BuildConfig       PagesProjectBuildConfig       `json:"build_config"`
	Source            PagesProjectSource            `json:"source"`
}

// PagesProjectDeploymentStage represents an individual stage in a Pages project deployment.
type PagesProjectDeploymentStage struct {
	Name      string     `json:"name"`
	StartedOn *time.Time `json:"started_on,omitempty"`
	EndedOn   *time.Time `json:"ended_on,omitempty"`
	Status    string     `json:"status"`
}

// PagesProjectDeploymentTrigger represents information about what caused a deployment.
type PagesProjectDeploymentTrigger struct {
	Type     string                                 `json:"type"`
	Metadata *PagesProjectDeploymentTriggerMetadata `json:"metadata"`
}

// PagesProjectDeploymentTriggerMetadata represents additional information about the cause of a deployment.
type PagesProjectDeploymentTriggerMetadata struct {
	Branch        string `json:"branch"`
	CommitHash    string `json:"commit_hash"`
	CommitMessage string `json:"commit_message"`
}

type pagesProjectResponse struct {
	Response
	Result PagesProject `json:"result"`
}

type pagesProjectListResponse struct {
	Response
	Result     []PagesProject `json:"result"`
	ResultInfo `json:"result_info"`
}

// ListPagesProjects returns all Pages projects for an account.
//
// API reference: https://api.cloudflare.com/#pages-project-get-projects
func (api *API) ListPagesProjects(ctx context.Context, accountID string, pageOpts PaginationOptions) ([]PagesProject, ResultInfo, error) {
	v := url.Values{}
	if pageOpts.PerPage > 0 {
		v.Set("per_page", strconv.Itoa(pageOpts.PerPage))
	}
	if pageOpts.Page > 0 {
		v.Set("page", strconv.Itoa(pageOpts.Page))
	}

	uri := fmt.Sprintf("/accounts/%s/pages/projects", accountID)
	if len(v) > 0 {
		uri = fmt.Sprintf("%s?%s", uri, v.Encode())
	}

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return []PagesProject{}, ResultInfo{}, err
	}
	var r pagesProjectListResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return []PagesProject{}, ResultInfo{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, r.ResultInfo, nil
}

// PagesProject returns a single Pages project by name.
//
// API reference: https://api.cloudflare.com/#pages-project-get-project
func (api *API) PagesProject(ctx context.Context, accountID, projectName string) (PagesProject, error) {
	uri := fmt.Sprintf("/accounts/%s/pages/projects/%s", accountID, projectName)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return PagesProject{}, err
	}
	var r pagesProjectResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return PagesProject{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// CreatePagesProject creates a new Pages project in an account.
//
// API reference: https://api.cloudflare.com/#pages-project-create-project
func (api *API) CreatePagesProject(ctx context.Context, accountID string, pagesProject PagesProject) (PagesProject, error) {
	uri := fmt.Sprintf("/accounts/%s/pages/projects", accountID)
	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, pagesProject)
	if err != nil {
		return PagesProject{}, err
	}
	var r pagesProjectResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return PagesProject{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// UpdatePagesProject updates an existing Pages project.
//
// API reference: https://api.cloudflare.com/#pages-project-update-project
func (api *API) UpdatePagesProject(ctx context.Context, accountID, projectName string, pagesProject PagesProject) (PagesProject, error) {
	uri := fmt.Sprintf("/accounts/%s/pages/projects/%s", accountID, projectName)
	res, err := api.makeRequestContext(ctx, http.MethodPatch, uri, pagesProject)
	if err != nil {
		return PagesProject{}, err
	}
	var r pagesProjectResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return PagesProject{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// DeletePagesProject deletes a Pages project by name.
//
// API reference: https://api.cloudflare.com/#pages-project-delete-project
func (api *API) DeletePagesProject(ctx context.Context, accountID, projectName string) error {
	uri := fmt.Sprintf("/accounts/%s/pages/projects/%s", accountID, projectName)
	res, err := api.makeRequestContext(ctx, http.MethodDelete, uri, nil)
	if err != nil {
		return err
	}
	var r pagesProjectResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return errors.Wrap(err, errUnmarshalError)
	}
	return nil
}
