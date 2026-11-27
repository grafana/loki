# Security Policy

Please read this Security Policy fully before reporting or disclosing a vulnerability.

Do not disclose a security vulnerability in public.  Instead, report it privately by email using the process outlined below so that a fix can be prepared before disclosure.

Credit (e.g., thanks or security advisory credit) is a courtesy and may be withheld or removed, for example, if someone does not follow this Security Policy or the [Contribution Guidelines](https://github.com/fxamacker/cbor/blob/master/CONTRIBUTING.md), or violates the [Code of Conduct](https://github.com/fxamacker/cbor/blob/master/CODE_OF_CONDUCT.md).

## Prohibited Content

Please do not submit or transmit personal data, secrets, nonpublic information, malicious code, malicious links, or any data that could create regulatory compliance obligations, legal liabilities, or other risks for the recipient.

Your own name and your email address are exempt from this section.

## Vulnerability Reporting Process

Please note that unvetted AI-generated security reports and reproducers can delay fixes for real bugs.

1. Before reporting a bug, please ensure a human confirms it matters in real use and any reproducer code is valid.  Some AI-detected bugs only occur when AI-written reproducers use anti-patterns or settings that don't reflect real user apps in production environments.

2. Email a brief report and minimal reproducer to faye.github@gmail.com. To reduce delays, follow these guidelines:
   - Begin the email subject with "fxamacker/cbor vulnerability".
   - Keep the report accurate and preferably under one page (omit AI-generated filler).
   - Include the results of testing the reproducer with both the latest release and the current default branch.
   - Write the reproducer as a single file that doesn't import other third-party packages.
   - Use the simplest data types and data structures that reproduce the bug.
   - Keep the CBOR data as small as possible (max 4096 bytes of CBOR data).
   - Do not send download links or binary files (attach only .go or .txt files).

3. Do not disclose the vulnerability publicly until a fix is released or until the disclosure date agreed with the maintainer, whichever comes first.

Even a one-line security fix requires testing, fuzzing, backward compatibility checks, etc. before the pull request is opened in public.

## Support Agreements with Response Times

If you require a guaranteed response time or rely on this library for mission-critical operations, please consider a dedicated support agreement or sponsorship to fund ongoing maintenance and improvements.

Under a support agreement, client-specific features, backports of fixes to older releases of this library, or support for older Go versions can be accommodated in separate branches or private repositories.
