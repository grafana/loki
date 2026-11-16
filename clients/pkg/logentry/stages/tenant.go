package stages

import (
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/clients/pkg/util"
)

const (
	ErrTenantStageEmptyLabelSourceOrValue        = "label, source or value config are required"
	ErrTenantStageConflictingLabelSourceAndValue = "label, source and value are mutually exclusive: you should set source, value or label but not all"
)

type tenantStage struct {
	cfg TenantConfig
}

type TenantConfig struct {
	Label  string `mapstructure:"label"`
	Source string `mapstructure:"source"`
	Value  string `mapstructure:"value"`
}

// validateTenantConfig validates the tenant stage configuration
func validateTenantConfig(c TenantConfig) error {
	if c.Source == "" && c.Value == "" && c.Label == "" {
		return errors.New(ErrTenantStageEmptyLabelSourceOrValue)
	}

	if c.Source != "" && c.Value != "" || c.Label != "" && c.Value != "" || c.Source != "" && c.Label != "" {
		return errors.New(ErrTenantStageConflictingLabelSourceAndValue)
	}

	return nil
}

// newTenantStage creates a new tenant stage to override the tenant ID from extracted data
func newTenantStage(configs interface{}) (Stage, error) {
	cfg := TenantConfig{}
	err := mapstructure.Decode(configs, &cfg)
	if err != nil {
		return nil, err
	}

	err = validateTenantConfig(cfg)
	if err != nil {
		return nil, err
	}

	return toStage(&tenantStage{
		cfg: cfg,
	}), nil
}

// Process implements Stage
func (s *tenantStage) Process(labels model.LabelSet, extracted map[string]interface{}, _ *time.Time, _ *string) {
	var tenantID string

	// Get tenant ID from source or configured value
	if s.cfg.Source != "" {
		tenantID = s.getTenantFromSourceField(extracted)
	} else if s.cfg.Label != "" {
		tenantID = s.getTenantFromLabel(labels)
	} else {
		tenantID = s.cfg.Value
	}

	// Skip an empty tenant ID (ie. failed to get the tenant from the source)
	if tenantID == "" {
		return
	}

	labels[util.ReservedLabelTenantID] = model.LabelValue(tenantID)
}

// Name implements Stage
func (s *tenantStage) Name() string {
	return StageTypeTenant
}

func (s *tenantStage) getTenantFromSourceField(extracted map[string]interface{}) string {
	// Get the tenant ID from the source data
	value, ok := extracted[s.cfg.Source]
	if !ok {
		return ""
	}

	// Convert the value to string
	tenantID, err := getString(value)
	if err != nil {
		return ""
	}

	return tenantID
}

func (s *tenantStage) getTenantFromLabel(labels model.LabelSet) string {
	// Get the tenant ID from the label map
	tenantID, ok := labels[model.LabelName(s.cfg.Label)]

	if !ok {
		return ""
	}

	return string(tenantID)
}
