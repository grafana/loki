# Project context (local)

<!-- Rename to `docs/project-context.md` after copying (see SETUP.md for alternatives). -->

## Identity

- **Product name (short):** Loki
- **Product name (first mention in prose):** Grafana Loki
- **GitHub org/repo:** https://github.com/grafana/loki

## Branches and releases

- **Default development branch:** `main`
- **Release branch pattern:** `release-X.Y.x`
- **Docs version mapping:** <!-- how doc site version maps to product version -->

## Documentation paths

- **Documentation root (filesystem):** `loki/docs/sources/`
- **Generated pages (do not hand-edit):**
   - /docs/sources/configure/_index.md
   -/docs/sources/reference/loki-config-reference.md
- **Configuration reference index:** /docs/sources/configure/_index.md
- **Changelog:** `CHANGELOG.md` at repo root
- **Architecture / "start here" page:**

## Helm chart

<!-- Only relevant for the helm-chart-docs skill. Distinguish grafana/helm-charts (Grafana-maintained charts) from grafana-community/helm-charts (community-maintained charts, where Loki's chart lives). -->

- **Helm chart repository:** `https://github.com/grafana-community/helm-charts` (source of truth; a **separate** repo from this one, commonly a sibling checkout, e.g. `../helm-charts`)
- **Chart path in that repo:** `charts/loki`
- **Helm docs location (filesystem):** `loki/docs/sources/setup/install/helm/`
- **Generated Helm values reference (do not hand-edit):** `loki/docs/sources/setup/install/helm/reference.md` (regenerated from `charts/loki/values.yaml` in the Helm chart repository above)

## Code ↔ documentation mapping

| Code area | Documentation area |
|-----------|---------------------|
| e.g. `pkg/api/` | e.g. `docs/.../api/` |
| e.g. `internal/config/` | e.g. `docs/.../configuration/` |

## Code validation paths

Paths the agent should check when validating documentation claims against code.

| What to validate | Where to look |
|-----------------|---------------|
| Query/syntax correctness | e.g. `pkg/traceql/test_examples.yaml` |
| Type definitions / intrinsics | e.g. `pkg/traceql/ast.go` |
| Configuration structs / defaults | e.g. `internal/config/` or `modules/` |

## Frontmatter and site conventions

- Default `topicType` or template names:
- Weight / ordering rules:
- Internal link style (trailing slash, etc.):

## Conventions for agents

- **Query language / API naming** (exact terms):
- **Storage or format version naming** (if any):
- **Vale or linter config location** (if any):

## Subsystem knowledge

- Link or path to code-adjacent `AGENTS.md` files (e.g. generator, storage backend) when docs work touches those areas.

## Optional: shared features across sub-products

If applicable, list features that require coordinated updates across multiple doc trees.
