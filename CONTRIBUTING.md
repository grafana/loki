# Contribute

Loki uses GitHub to manage reviews of pull requests:

- If you have a trivial fix or improvement, go ahead and create a pull request.
- If you plan to do something more involved, discuss your ideas on the relevant GitHub issue.
- Make sure to follow the prerequisites below before marking your PR as ready for review.

## Pull Request Prerequisites/Checklist

1. Your PR title is in the form `<Feature Area>: Your change`.
  a. It does not end the title with punctuation. It will be added in the changelog.
  b. It starts with an imperative verb. Example: Fix the latency between System A and System B.
  c. It uses Sentence case, not Title Case.
  d. It uses a complete phrase or sentence. The PR title will appear in a changelog, so help other people understand what your change will be.
  e. It has a clear description saying what it does and why. Your PR description will be present in the project' commit log, so be gentle to it.
2. Your PR is well sync'ed with main
3. Your PR is correctly documenting appropriate changes under the CHANGELOG. You should document your changes there if:
  * It adds an important feature
  * It fixes an issue present in a previous release
  * It causes a change in operation that would be useful for an operator of Loki to know then please add a CHANGELOG entry.
For documentation changes, build changes, simple fixes etc please skip this step. We are attempting to curate a changelog of the most relevant and important changes to be easier to ingest by end users of Loki.
4. Your PR documents upgrading steps under `docs/sources/upgrading/_index.md` if it changes:
  * default configuration values
  * metric names or label names
  * changes existing log lines such as the metrics.go query output line
  * configuration parameters
  * anything to do with any API
  * any other change that would require special attention or extra steps to upgrade
Please document clearly what changed AND what needs to be done in the upgrade guide.

## Contribute to helm

Please follow the [Loki Helm Chart](./production/helm/loki/README.md).

### Dependency management

We use [Go modules](https://golang.org/cmd/go/#hdr-Modules__module_versions__and_more) to manage dependencies on external packages.
This requires a working Go environment with version 1.15 or greater and git installed.

To add or update a new dependency, use the `go get` command:

```bash
# Pick the latest tagged release.
go get example.com/some/module/pkg

# Pick a specific version.
go get example.com/some/module/pkg@vX.Y.Z
```

Tidy up the `go.mod` and `go.sum` files:

```bash
go mod tidy
go mod vendor
git add go.mod go.sum vendor
git commit
```

You have to commit the changes to `go.mod` and `go.sum` before submitting the pull request.

## Coding Standards

### go imports
imports should follow `std libs`, `externals libs` and `local packages` format

Example
```
import (
	"fmt"
	"math"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
)
```
