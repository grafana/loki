package godo

import (
	"context"
	"fmt"
	"net/http"
)

const (
	securityScansBasePath     = "v2/security/scans"
	securityScansFindingsPath = "findings"
	securityAffectedResources = "affected_resources"
)

// SecurityService is an interface for interacting with the CSPM endpoints of
// the DigitalOcean API.
type SecurityService interface {
	CreateScan(context.Context, *CreateScanRequest) (*Scan, *Response, error)
	ListScans(context.Context, *ListOptions) ([]*Scan, *Response, error)
	GetScan(context.Context, string, *ScanFindingsOptions) (*Scan, *Response, error)
	GetLatestScan(context.Context, *ScanFindingsOptions) (*Scan, *Response, error)
	ListFindingAffectedResources(context.Context, *ListFindingAffectedResourcesRequest, *ListOptions) ([]*AffectedResource, *Response, error)
}

// SecurityServiceOp handles communication with security scan related methods of the DigitalOcean API.
type SecurityServiceOp struct {
	client *Client
}

var _ SecurityService = &SecurityServiceOp{}

// CreateScanRequest contains the request payload to create a scan.
type CreateScanRequest struct {
	Resources []string `json:"resources,omitempty"`
}

// ScanFindingsOptions contains the query parameters for paginating and
// filtering scan findings.
type ScanFindingsOptions struct {
	ListOptions
	Type     string `url:"type,omitempty"`
	Severity string `url:"severity,omitempty"`
}

// Scan represents a CSPM scan.
type Scan struct {
	ID        string         `json:"id,omitempty"`
	Status    string         `json:"status,omitempty"`
	CreatedAt string         `json:"created_at,omitempty"`
	Findings  []*ScanFinding `json:"findings,omitempty"`
}

func (s *Scan) Completed() bool {
	return s.Status == "COMPLETED"
}

// ScanFinding represents a finding within a scan.
type ScanFinding struct {
	RuleUUID               string                `json:"rule_uuid,omitempty"`
	Name                   string                `json:"name,omitempty"`
	Details                string                `json:"details,omitempty"`
	FoundAt                string                `json:"found_at,omitempty"`
	Severity               string                `json:"severity,omitempty"`
	BusinessImpact         string                `json:"business_impact,omitempty"`
	TechnicalDetails       string                `json:"technical_details,omitempty"`
	MitigationSteps        []*ScanMitigationStep `json:"mitigation_steps,omitempty"`
	AffectedResourcesCount int                   `json:"affected_resources_count,omitempty"`
}

// ScanMitigationStep represents a mitigation step for a scan finding.
type ScanMitigationStep struct {
	Step        int    `json:"step,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// An AffectedResource represents a resource affected by a scan finding.
type AffectedResource struct {
	URN  string `json:"urn,omitempty"`
	Name string `json:"name,omitempty"`
	Type string `json:"type,omitempty"`
}

type scanRoot struct {
	Scan *Scan `json:"scan"`
}

type scansRoot struct {
	Scans []*Scan `json:"scans"`
	Links *Links  `json:"links"`
	Meta  *Meta   `json:"meta"`
}

type affectedResourcesRoot struct {
	AffectedResources []*AffectedResource `json:"affected_resources"`
	Links             *Links              `json:"links"`
	Meta              *Meta               `json:"meta"`
}

// CreateScan initiates a new CSPM scan.
func (s *SecurityServiceOp) CreateScan(ctx context.Context, createScanRequest *CreateScanRequest) (*Scan, *Response, error) {
	if createScanRequest == nil {
		return nil, nil, NewArgError("createScanRequest", "cannot be nil")
	}

	req, err := s.client.NewRequest(ctx, http.MethodPost, securityScansBasePath, createScanRequest)
	if err != nil {
		return nil, nil, err
	}

	scan := &Scan{}
	resp, err := s.client.Do(ctx, req, scan)
	if err != nil {
		return nil, resp, err
	}

	return scan, resp, nil
}

// ListScans lists all CSPM scans.
func (s *SecurityServiceOp) ListScans(ctx context.Context, opts *ListOptions) ([]*Scan, *Response, error) {
	path, err := addOptions(securityScansBasePath, opts)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(scansRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if l := root.Links; l != nil {
		resp.Links = l
	}
	if m := root.Meta; m != nil {
		resp.Meta = m
	}

	return root.Scans, resp, nil
}

// GetScan retrieves a scan by its UUID with optional findings filters.
func (s *SecurityServiceOp) GetScan(ctx context.Context, scanUUID string, opts *ScanFindingsOptions) (*Scan, *Response, error) {
	if scanUUID == "" {
		return nil, nil, NewArgError("scanUUID", "cannot be empty")
	}

	path := fmt.Sprintf("%s/%s", securityScansBasePath, scanUUID)
	path, err := addOptions(path, opts)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := &scanRoot{}
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Scan, resp, nil
}

// GetLatestScan retrieves the latest scan with optional findings filters.
func (s *SecurityServiceOp) GetLatestScan(ctx context.Context, opts *ScanFindingsOptions) (*Scan, *Response, error) {
	path := fmt.Sprintf("%s/latest", securityScansBasePath)
	path, err := addOptions(path, opts)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := &scanRoot{}
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Scan, resp, nil
}

// ListFindingAffectedResourcesRequest contains the fields to list the
// affected resources for a scan finding.
type ListFindingAffectedResourcesRequest struct {
	ScanUUID    string
	FindingUUID string
}

// ListFindingAffectedResources lists the affected resources for a scan
// finding.
func (s *SecurityServiceOp) ListFindingAffectedResources(ctx context.Context, listFindingAffectedResourcesRequest *ListFindingAffectedResourcesRequest, opts *ListOptions) ([]*AffectedResource, *Response, error) {
	if listFindingAffectedResourcesRequest == nil {
		return nil, nil, NewArgError("listFindingAffectedResourcesRequest", "cannot be nil")
	}
	if listFindingAffectedResourcesRequest.ScanUUID == "" {
		return nil, nil, NewArgError("scanUUID", "cannot be empty")
	}
	if listFindingAffectedResourcesRequest.FindingUUID == "" {
		return nil, nil, NewArgError("findingUUID", "cannot be empty")
	}

	path := fmt.Sprintf(
		"%s/%s/%s/%s/%s",
		securityScansBasePath,
		listFindingAffectedResourcesRequest.ScanUUID,
		securityScansFindingsPath,
		listFindingAffectedResourcesRequest.FindingUUID,
		securityAffectedResources,
	)
	path, err := addOptions(path, opts)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := &affectedResourcesRoot{}
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if l := root.Links; l != nil {
		resp.Links = l
	}
	if m := root.Meta; m != nil {
		resp.Meta = m
	}

	return root.AffectedResources, resp, nil
}
