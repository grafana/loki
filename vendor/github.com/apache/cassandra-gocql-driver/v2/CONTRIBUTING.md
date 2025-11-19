# Contributing to the Apache Cassandra GoCQL Driver

**TL;DR** This manifesto sets out the bare minimum requirements for submitting a patch to gocql. It also offers some suggestions to speed up the review, approve and merge process.

This guide outlines the process of landing patches in gocql and the general approach to maintaining the code base.

## Background

The goal of the gocql project is to provide a stable and robust CQL driver for Go. This is a community driven project that is coordinated by a small team of developers in and around the Apache Cassandra project. For security, governance and administration issues please refer to the Cassandra Project Management Committee.

## Engage with the community early

If you are interested in contributing a particular feature or bug fix we heavily encourage you to start a discussion with the community or at the very least announce your interest in working on it before jumping right into writing code. It helps reduce the likelihood of multiple people working on the same issue in parallel when they could be collaborating instead. Getting feedback early in the contribution process will also greatly speed up the review, approval and merge process. 

Common ways to engage with the GoCQL community are: 

- [CASSGO project on ASF JIRA](https://issues.apache.org/jira/projects/CASSGO/issues/)
- Apache Cassandra dev mailing list - [dev@cassandra.apache.org](mailto:dev@cassandra.apache.org)
  - [Subscribe](mailto:dev-subscribe@cassandra.apache.org)
  - [Archives](https://lists.apache.org/list.html?dev@cassandra.apache.org)
- [#cassandra-drivers channel on ASF Slack](https://the-asf.slack.com/archives/C05LPRVNZV1)

## Minimum Requirement Checklist

The following is a check list of requirements that need to be satisfied in order for us to merge your patch:

* A JIRA issue exists in the [CASSGO Project](https://issues.apache.org/jira/projects/CASSGO/issues/) for the proposed changes
  * If the proposed changes are significant then ideally a discussion about the implementation approach should happen before a PR is even opened (prototyping is fine though)
* Pull request raised to apache/cassandra-gocql-driver on Github
* The pull request has a title that clearly summarizes the purpose of the patch and references the relevant CASSGO JIRA issue if there is one
* The motivation behind the patch is clearly defined in the pull request summary
* JIRA issue is added to the "UNRELEASED" section of CHANGELOG.md
  * If there's no JIRA issue yet then the author is encouraged to create it
  * If the author is not able to create the JIRA issue then the committer that merges the PR will take care of this (creating the JIRA and adding it to CHANGELOG.md)
  * Updating CHANGELOG.md is not required if the change is not "releasable" (e.g. changes to documentation, CI, etc.)
* You agree that your contribution is donated to the Apache Software Foundation (appropriate copyright is on all new files)
* The patch will merge cleanly
* The test coverage does not fall
* The merge commit passes the regression test suite on GitHub Actions
* `go fmt` has been applied to the submitted code
* Functional changes are documented in godoc
* A correctly formatted commit message, see below

If there are any requirements that can't be reasonably satisfied, please state this either on the pull request or as part of discussion on the mailing list, JIRA or slack. Where appropriate, the core team may apply discretion and make an exception to these requirements.

## Commit Message

The commit message format should be:

```
<short description>

<reason why the change is needed>

Patch by <authors>; reviewed by <Reviewers> for CASSGO-#####
```

Short description should:
* Be a short sentence.
* Start with a capital letter.
* Be written in the present tense.
* Summarize what is changed, not why it is changed.

Short description should not:
* End with a period.
* Use the word Fixes . Most commits fix something.

Long description / Reason:
* Should describe why the change is needed. What is fixed by the change? Why it it was broken before? What use case does the new feature solve?
* Consider adding details of other options that you considered when implementing the change and why you made the design decisions you made.

The `patch by …; reviewed by` line is important. It is parsed to build the [project contribulyse statistics](https://nightlies.apache.org/cassandra/devbranch/misc/contribulyze/html/).

Some tips from the Apache Cassandra Project's "How to Commit" documentation: https://cassandra.apache.org/_/development/how_to_commit.html#tips

#### Example commit message:

```
Increase default timeouts to 11s

Client timeouts need to be higher than server timeouts,
so that work does not accumulate on the server with retries.
If the client timeout is shorter than a server timeout,
the client can start a retry while the original request
is still running on the server.

The default gocql default timeout was lower
than the Cassandra default timeout.

Cassandra has multiple server timeout options,
most of them are less or equal to 10s by default as of Cassandra 4.1:

read_request_timeout           5s
range_request_timeout          10s
write_request_timeout          2s
counter_write_request_timeout  5s
cas_contention_timeout         1s
truncate_request_timeout       60s
request_timeout                10s

Truncate is an uncommon operation, so we can use 11s as the default
timeout.

patch by John Doe, Jane Doe; reviewed by Bob Smith, Jane Smith for CASSGO-#####
```

### Signing commits

Signing commits with a pgp or ssh key is heavily encouraged although not required.

## Beyond The Checklist

In addition to stating the hard requirements, there are a bunch of things that we consider when assessing changes to the library. These soft requirements are helpful pointers of how to get a patch landed quicker and with less fuss.

### General QA Approach

The Cassandra project needs to consider the ongoing maintainability of the library at all times. Patches that look like they will introduce maintenance issues for the team will not be accepted.

Your patch will get merged quicker if you have decent test cases that provide test coverage for the new behavior you wish to introduce.

Unit tests are good, integration tests are even better. An example of a unit test is `marshal_test.go` - this tests the serialization code in isolation. `cassandra_test.go` is an integration test suite that is executed against every version of Cassandra that gocql supports as part of the CI process on Travis.

That said, the point of writing tests is to provide a safety net to catch regressions, so there is no need to go overboard with tests. Remember that the more tests you write, the more code we will have to maintain. So there's a balance to strike there.

### Sign Off Procedure

A Pull Request needs +1s from two committers before it can be merged (or one +1 if the author is a committer).

As stated earlier, suitable test coverage will increase the likelihood that a PR will be approved and merged. If your change has no test coverage, or looks like it may have wider implications for the health and stability of the library, the reviewers may elect to refer the change to other members of the community to achieve consensus before proceeding. Therefore, the tighter and cleaner your patch is, the quicker it will go through the review process.

### Supported Features

gocql is a low level wire driver for Cassandra CQL. By and large, we would like to keep the functional scope of the library as narrow as possible. We think that gocql should be tight and focused, and we will be naturally skeptical of things that could just as easily be implemented in a higher layer. 

Inevitably you will come across something that could be implemented in a higher layer, save for a minor change to the core API. In this instance, please strike up a conversation in the Cassandra community. 

Chances are we will understand what you are trying to achieve and will try to accommodate this in a maintainable way.
