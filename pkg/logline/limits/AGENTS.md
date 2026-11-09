# pkg/logline/limits/

Per-tenant settings interface for logline components.

## Invariants

- The Limits interface contains only the methods each component actually needs.
  Do not add methods here unless a logline component requires them.
- RetentionPeriod() == 0 → retention is disabled (keep forever)
- The Limits returned by NewMaxLimits computes the **maximum** across all
  configured tenants. Logline indexes are shared across tenants, so the most
  permissive (longest) value must apply. A zero from any tenant means "no override,
  use default" and is skipped by computeMax().
- Value resolution priority (highest to lowest):
  1. Per-tenant runtime overrides (max across all tenants)
  2. Loki config `limits_config` defaults (retention_period)
- NewMaxLimits wraps Loki's `*validation.Overrides`. It owns no runtime config
  manager: Loki's runtime config reloads the tenant limits, and every
  RetentionPeriod call reads the current values.
