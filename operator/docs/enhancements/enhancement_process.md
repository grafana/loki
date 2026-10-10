---
title: Enhancement Process
authors:
  - @JoaoBraveCoding
creation-date: 2026-07-21
last-updated: 2026-07-21
state: published
---

## Summary

Enhancement proposals describe significant changes to the Loki Operator — new features, API extensions, architectural decisions, and process changes. Each proposal tracks its lifecycle through a `state` field in its frontmatter, making it clear where an idea stands and whether it is ready to merge, implement, or abandon.

## Metadata

Every enhancement document must include a `state` field in its YAML frontmatter:

```yaml
---
title: my-enhancement
state: draft  # One of: draft, discussion, published, committed, abandoned
authors:
  - @author
...
---
```

## States

An enhancement can be in one of the following five states:

### draft

Work in progress; not yet ready for formal review. The author is still iterating on the idea, gathering context, or filling in design details. This covers both early ideation and active authoring.

### discussion

Open for review, typically via a pull request. The proposal is sufficiently formed for feedback from reviewers and the broader team. Discussion happens on the PR and design meetings; the author incorporates feedback as appropriate.

### published

Reviewed and merged into the repository. The proposal represents an agreed-upon direction but has not yet been fully implemented. Published enhancements can still be updated through follow-up pull requests.

### committed

Fully implemented. The design described in the enhancement has been delivered in the operator. Changes to committed enhancements should be infrequent; significant divergences may warrant a new enhancement proposal.

### abandoned

Deliberately not implemented, or superseded by another proposal. Abandoned enhancements remain in the repository as a record of what was considered and why it was not pursued.

## Lifecycle

The typical flow between states:

```
draft → discussion → published → committed
                  ↘ abandoned
```

- A new enhancement starts in **draft**.
- When ready for review, move to **discussion** and open a pull request.
- When the PR is approved and merged, move to **published**.
- Once the feature is fully implemented, move to **committed**.
- At any point before implementation, an enhancement can move to **abandoned** if the idea is rejected or superseded.

When changing the state of an enhancement, update the `state` field in the frontmatter and the `last-updated` date.
