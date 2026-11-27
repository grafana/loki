# Contributing to fxamacker/cbor

Please read these guidelines fully before opening an issue or pull request.  Thanks!

These guidelines reduce security risks, operational risks, and legal risks from unvetted AI content, false positives, uncoordinated pull requests, and uncertain authorship.  Issues, pull requests, and comments that do not follow them may be closed or deleted.

Credit (e.g., thanks or security advisory credit) is a courtesy and may be withheld or removed, for example, if someone does not follow these guidelines or violates the [Code of Conduct](https://github.com/fxamacker/cbor/blob/master/CODE_OF_CONDUCT.md) or [Security Policy](https://github.com/fxamacker/cbor/blob/master/SECURITY.md).

## Prohibited Content

Do not submit or transmit personal data, secrets, nonpublic information, malicious code, malicious links, or any data that could create regulatory compliance obligations, legal liabilities, or other risks for the recipient.

Your own real name and your email address in commits (e.g., `Signed-off-by` lines) are exempt from this section.

## Security Vulnerabilities

Report security vulnerabilities privately by email as specified in the [Security Policy](https://github.com/fxamacker/cbor/blob/master/SECURITY.md).

Do not disclose them prematurely in public (e.g., issues, pull requests, comments, or social media).

## Opening Issues

Please ensure a human confirms the issue matters in real use before opening it.  Some AI-detected bugs only occur when AI-written reproducers use anti-patterns or settings that don't reflect real user apps in production environments, and fixing them can add overhead and complexity without practical benefit.

You can open an issue from the [Issues page](https://github.com/fxamacker/cbor/issues), or create one using these links:

- [Open a bug report](https://github.com/fxamacker/cbor/issues/new?template=1-bug-report.yml)
- [Open a feature request](https://github.com/fxamacker/cbor/issues/new?template=2-feature-request.yml)
- [Open a documentation issue](https://github.com/fxamacker/cbor/issues/new?template=3-docs-issue.yml)

## Opening Pull Requests (PRs)

Please read the [Pull Request Template](https://github.com/fxamacker/cbor/blob/master/.github/pull_request_template.md) fully before starting work or proposing the change.  You must follow all its requirements, including coordination steps, standards, compatibility, performance, and sign-off (DCO 1.1).

Although AI may be used to improve the PR description (e.g., to fix grammar), **a human must manually open PRs and personally make their own attestations without AI**.  These include adding `Signed-off-by` lines to commits, certifying the DCO 1.1, and confirming they personally reviewed and understand each changed line.

To ensure work is well-coordinated, please comment on the related issue saying you plan to work on it, and wait for a maintainer to reply that you can proceed before opening a PR.  If no issue exists yet, please open one first.  Even a one-line change can affect in-progress work, planned work, or ongoing maintenance (e.g., testing, fuzzing, and backward compatibility checks).

PRs opened by Dependabot that only update GitHub Actions workflows are exempt from this section.

## Special Thanks

- @lukseven for pointing out in 2021 that the contribution guidelines didn't mention signing requirements.
