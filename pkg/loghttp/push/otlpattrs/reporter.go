package otlpattrs

import (
	"flag"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/limiter"

	"github.com/grafana/loki/v3/pkg/runtime"
)

// Cfg shapes the attribute expansion reports for the tenants that have
// log_otlp_attribute_expansion set in their runtime config.
type Cfg struct {
	Rate          float64 `yaml:"rate"`
	MaxAttributes int     `yaml:"max_attributes"`
}

// RegisterFlagsWithPrefix registers the attribute expansion report flags.
func (cfg *Cfg) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.Float64Var(&cfg.Rate, prefix+".rate", 1, "Number of attribute expansion reports to emit per second, per tenant. Each report is one summary line plus one line per attribute.")
	f.IntVar(&cfg.MaxAttributes, prefix+".max-attributes", 20, "Maximum number of attributes to log on their own line for a reported request. The remaining attributes are summarised on a single overflow line.")
}

// Reporter emits rate limited reports of OTLP attribute expansion.
type Reporter struct {
	limiter       *limiter.RateLimiter
	tenantCfgs    *runtime.TenantConfigs
	maxAttributes int
}

// NewReporter returns a Reporter that emits at most cfg.Rate reports per second,
// for each tenant that has log_otlp_attribute_expansion set in its runtime
// config.
func NewReporter(cfg Cfg, tenants *runtime.TenantConfigs) *Reporter {
	burst := max(1, int(cfg.Rate))
	return &Reporter{
		limiter:       limiter.NewRateLimiter(newStrategy(burst, cfg.Rate), time.Hour),
		tenantCfgs:    tenants,
		maxAttributes: cfg.MaxAttributes,
	}
}

// Report emits one summary line for the request followed by one line per
// top-ranked attribute. Pass the request scoped logger so the report carries the
// same org_id and traceID as the rest of the push request's logging.
func (r *Reporter) Report(logger log.Logger, tenantID string, acc *Accumulator) {
	if r == nil || acc == nil {
		return
	}

	if !r.tenantCfgs.LogOTLPAttributeExpansion(tenantID) {
		return
	}

	if acc.IsEmpty() {
		// nothing to report
		return
	}

	if !r.limiter.AllowN(time.Now(), tenantID, 1) {
		return
	}

	report := acc.Report(r.maxAttributes)

	level.Info(logger).Log(
		"msg", "otlp attribute expansion",
		"num_records", report.Records,
		"num_attributes", report.Attributes,
		"attribute_expanded_bytes", report.AttributeExpandedBytes,
		"num_logged_attributes", len(report.Top),
		"num_overflow_attributes", report.OverflowAttributes,
		"overflow_expanded_bytes", report.OverflowExpandedBytes,
		"overflow_names", strings.Join(report.OverflowNames, ","),
	)

	for _, attr := range report.Top {
		level.Info(logger).Log(
			"msg", "otlp attribute",
			"kind", string(attr.Kind),
			"attribute", attr.Name,
			"records", attr.Records,
			"expanded_bytes", attr.ExpandedBytes,
		)
	}
}
