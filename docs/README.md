# Loki Documentation

This directory contains the source code for the Loki documentation.

Some key things to know about the Loki documentation source:
- The docs are written in markdown, specifically the CommonMark flavor of markdown.
- The Grafana docs team uses [Hugo](https://gohugo.io/) to generate the documentation.
- While you can view the documentation in GitHub, GitHub does not render the images or links correctly and cannot render the Hugo specific shortcodes. To read the Loki documentation, see the [Documentation Site](https://grafana.com/docs/loki/latest/).
- If you have a trivial fix or improvement, go ahead and create a pull request.
- If you plan to do something more involved, for example creating a new topic, discuss your ideas on the relevant GitHub issue.
- Pull requests are merged to main, and published to [Upcoming release](https://grafana.com/docs/loki/next/). If your change needs to be published to the [Latest release](https://grafana.com/docs/loki/latest/) before the next Loki release (that is, it needs to be published immediately), add the appropriate backport label to your PR.  

## Contributing

We're glad you're here to help make the Loki documentation even better for the Loki community.

Issues and contributions are **always welcome**! Don't feel shy about contributing. All input is welcome. No fix is too small.

If the documentation confuses you or you think something is missing in the docs, create an [issue](https://github.com/grafana/loki/issues).
If you find something that you think you can fix, please go ahead and contribute a pull request (PR). You don't need to ask permission.

The Loki documentation is written using the CommonMark flavor of markdown, including some extended features. For more information about markdown, you can see the [CommonMark specification](https://spec.commonmark.org/), and a [quick reference guide](https://commonmark.org/help/) for CommonMark.

If you have a GitHub account and you're just making a small fix, for example fixing a typo or updating an example, you can edit the topic in GitHub.

1. Find the topic in the Loki repo.
2. Click the pencil icon.
3. Enter your changes.
4. Click **Commit changes**. GitHub creates a pull request for you.
5. The Loki team uses [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/) for commit messages. Make sure your commit messages for doc updates start with `doc:`. 
6. If this is your first contribution to the Loki repository, you will need to sign the Contributor License Agreement (CLA) before your PR can be accepted.
**NOTE:** A member of the Loki repo maintainers must approve and run the continuous integration (CI) workflows for community contributions.
7. Add the `type/docs` label to identify your PR as a docs contribution.  This helps the documentation team track our work.
8. If your contribution needs to be added to the current release or previous releases, apply the appropriate `backport` label. You can find more information about backporting in the [Writers' toolkit](https://grafana.com/docs/writers-toolkit/review/backporting/).

For larger contributions, for example documenting a new feature or adding a new topic, consider running the project locally to see how the changes look like before making a pull request.

The docs team has created a [Writers' Toolkit](https://grafana.com/docs/writers-toolkit/) that documents how we write documentation at Grafana Labs. Writers' Toolkit contains information about how we structure documentation at Grafana, including templates for different types of topics, information about Hugo shortcodes that extend markdown to add additional features, and information about linters and other tools that we use to write documentation. Writers' Toolkit also includes our [Style Guide](https://grafana.com/docs/writers-toolkit/write/style-guide/).

Note that in Hugo the structure of the documentation is based on the folder structure of the documentation repository. The URL structure is generated based on the folder structure and file names. Try to avoid moving or renaming files, as this will break cross-references to those files. If you must move or rename files, run `make docs` as described below to find and fix broken links before you submit your pull request.

## Shared content

**NOTE:** As of Loki/GEL 3.0, there will be shared files between the Loki docs and the GEL docs. The Grafana Enterprise Logs documentation will pull in content from the Loki repo when publishing the GEL docs. Files that are shared between the two doc sets will contain a comment indicating that the content is shared.

For more information about shared content, see the [reuse content](https://grafana.com/docs/writers-toolkit/write/reuse-content/) section of the Writers' Toolkit.

For more information about building and testing documentation, see the [build and review](https://grafana.com/docs/writers-toolkit/review/) section of the Writers' Toolkit.

## Testing documentation

Loki uses the static site generator [Hugo](https://gohugo.io/) to generate the documentation. The Loki repository uses a continuous integration (CI) action to sync documentation to the [Grafana website](https://grafana.com/docs/loki/latest). The CI is triggered on every merge to main in the `docs` subfolder.

You can preview the documentation in GitHub, but GitHub does not render images or any of the Hugo shortcodes. However, you can preview the documentation locally after installing [Docker](https://www.docker.com/) or [Podman](https://podman.io/).

To get a local preview of the documentation:
1. Navigate to the directory with the documentation makefile, `/loki/docs`.
2. Run the command `make docs`. This uses the `grafana/docs` image which internally uses Hugo to generate the static site.
3. Open http://localhost:3002/docs/loki/latest/ to review your changes.

> Note that `make docs` uses a lot of memory. If it crashes, increase the memory allocated to Docker and try again.

For more information about building and testing documentation, see [build and review](https://grafana.com/docs/writers-toolkit/review/) section of the Writers' Toolkit
