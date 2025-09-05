# Contributing to the Loki Helm Chart

Thank you for your interest in contributing to the Loki Helm Chart! This document provides guidelines for contributing to ensure the chart remains maintainable, broadly useful, and accessible to the community.

For general Loki project contributions, please also see the main [Contributing Guide](../../../CONTRIBUTING.md).

## Contribution Guidelines

### What We Welcome

We encourage contributions that:

- **Improve general usability**: Features that benefit the majority of users
- **Fix bugs**: Clear bug fixes with reproduction steps
- **Enhance documentation**: Better examples, clearer explanations
- **Security improvements**: Security-related enhancements
- **Performance optimizations**: Changes that improve performance broadly
- **Standards compliance**: Updates to follow Kubernetes best practices

### What We Avoid

To keep the chart maintainable and broadly useful, we avoid:

- **Project-specific integrations**: Features specific to other projects that serve only a small group of users
- **Company/individual-specific requirements**: Customizations that serve only specific organizations
- **Bizarre constraints**: Unusual or overly complex requirements that don't serve the general community
- **Breaking changes**: Changes that would break existing deployments without clear migration paths

### Decision Criteria

When reviewing contributions, we ask:

1. **Is this generally useful?** Does it benefit the broader community or just specific users?
2. **Is it simple and reasonable?** Does it add unnecessary complexity?
3. **Does it align with Kubernetes best practices?** Is it following established patterns?
4. **Is it maintainable?** Can the community maintain this feature long-term?

**Important**: If your use case is highly specific to your organization, consider using the Loki chart as a subchart instead of being called directly. This allows you to customize without adding complexity to the main chart.

## Technical Requirements

### Before Submitting a PR

1. **Update documentation**: Run `make helm-docs` from the repository root if you modified:
   - `Chart.yaml`
   - `values.yaml`
   - Any template files

2. **Update the changelog**: Add an entry to [CHANGELOG.md](./CHANGELOG.md) in the `Unreleased` section using the format:
   ```
   - [CHANGE|FEATURE|BUGFIX|ENHANCEMENT] Brief description of your change, and a link to the PR number. For examples, see the CHANGELOG.
   ```

3. **Test your changes**: The CICD workflow will run comprehensive tests, however it's a good idea to run a few "quick and dirty" tests locally before committing, as those workflows can take quite a while in comparison.
   - Single binary: `helm template --values single-binary-values.yaml`
   - Simple scalable: `helm template --values simple-scalable-values.yaml`
   - Distributed: `helm template --values distributed-values.yaml`

4. **Commit your changes**: Our commits follow the style of [Conventional Commits](https://www.conventionalcommits.org/).  High level this looks like `<type>: description`.  For example:
   - `feat(helm): add resource limits to querier`
   - `fix(helm): correct service selector`

Thank you for contributing to the Loki Helm Chart! Your contributions help make log aggregation accessible to the entire community.
