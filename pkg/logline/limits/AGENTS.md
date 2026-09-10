# pkg/logline/limits/

Per-tenant settings interface, construction, and runtime reloading for logline components.

## Invariants

- The Limits interface contains only the methods each component actually needs.
  Do not add methods here unless a logline component requires them.
- RetentionPeriod() == 0 → retention is disabled (keep forever)
- The Limits returned by NewOverrides computes the **maximum** across all
  configured tenants. Logline indexes are shared across tenants, so the most
  permissive (longest) value must apply. A zero from any tenant means "no override,
  use default" and is skipped by computeMax().
- Value resolution priority (highest to lowest):
  1. Per-tenant runtime overrides (max across all tenants)
  2. Loki config `limits_config` defaults (retention_period)
- `NewOverrides` consumes whatever defaults are already present in
  `loki.ConfigWrapper`. It does not apply ad-hoc per-field backfills; defaults
  are expected to have been initialized by the caller's config loading, normally
  via `flagext.DefaultValues`.
- NewOverrides accepts a loki.ConfigWrapper. It handles:
  1. SetDefaultLimitsForYAMLUnmarshalling (so per-tenant YAML inherits global defaults)
  2. runtimeconfig.Manager creation for periodic file polling
  3. *validation.Overrides construction (with or without TenantLimits)
- The returned services.Service (runtime config manager) may be nil when no override
  file is configured. When non-nil the caller must add it to the service manager.
- The returned *validation.Overrides is the raw Loki type for components that need
  per-tenant behavior (e.g., queryfrontend tripperware).
