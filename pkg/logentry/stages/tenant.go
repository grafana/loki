package stages

import (
	"reflect"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/promtail/client"
)

const (
	ErrTenantStageEmptySourceOrValue        = "source or value config are required"
	ErrTenantStageConflictingSourceAndValue = "source and value are mutually exclusive: you should set source or value but not both"
)

type tenantStage struct {
	cfg    TenantConfig
	logger log.Logger
}

type TenantConfig struct {
	Source string `mapstructure:"source"`
	Value  string `mapstructure:"value"`
}

// validateTenantConfig validates the tenant stage configuration
func validateTenantConfig(c TenantConfig) error {
	if c.Source == "" && c.Value == "" {
		return errors.New(ErrTenantStageEmptySourceOrValue)
	}

	if c.Source != "" && c.Value != "" {
		return errors.New(ErrTenantStageConflictingSourceAndValue)
	}

	return nil
}

// newTenantStage creates a new tenant stage to override the tenant ID from extracted data
func newTenantStage(logger log.Logger, configs interface{}) (Stage, error) {
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
		cfg:    cfg,
		logger: logger,
	}), nil
}

// Process implements Stage
func (s *tenantStage) Process(labels model.LabelSet, extracted map[string]interface{}, t *time.Time, entry *string) {
	var tenantID string

	// Get tenant ID from source or configured value
	if s.cfg.Source != "" {
		tenantID = s.getTenantFromSourceField(extracted)
	} else {
		tenantID = s.cfg.Value
	}

	// Skip an empty tenant ID (ie. failed to get the tenant from the source)
	if tenantID == "" {
		return
	}

	labels[client.ReservedLabelTenantID] = model.LabelValue(tenantID)
}

// Name implements Stage
func (s *tenantStage) Name() string {
	return StageTypeTenant
}

func (s *tenantStage) getTenantFromSourceField(extracted map[string]interface{}) string {
	// Get the tenant ID from the source data
	value, ok := extracted[s.cfg.Source]
	if !ok {
		if Debug {
			level.Debug(s.logger).Log("msg", "the tenant source does not exist in the extracted data", "source", s.cfg.Source)
		}
		return ""
	}

	// Convert the value to string
	tenantID, err := getString(value)
	if err != nil {
		if Debug {
			level.Debug(s.logger).Log("msg", "failed to convert value to string", "err", err, "type", reflect.TypeOf(value))
		}
		return ""
	}

	return tenantID
}
