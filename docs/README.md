# Loki Documentation

This directory contains the source code for the Loki documentation.

Some key things to know about the Loki documentation source:
- The docs are written in markdown, specifically the CommonMark flavor of markdown.
- The Grafana docs team uses [Hugo](https://gohugo.io/) to generate the documentation.
- While you can view the documentation in GitHub, GitHub does not render the images or links correctly and cannot render the Hugo specific shortcodes. To read the Loki documentation, see the [Documentation Site](https://grafana.com/docs/loki/latest/).
- If you have a trivial fix or improvement, go ahead and create a pull request.
- If you plan to do something more involved, for example creating a new topic, discuss your ideas on the relevant GitHub issue.

## Contributing

We're glad you're here to help make the Loki documentation even better for the Loki community.

Issues and contributions are **always welcome**! Don't feel shy about contributing. All input is welcome. No fix is too small.

If the documentation confuses you or you think something is missing in the docs, create an [issue](https://github.com/grafana/loki/issues).
If you find something that you think you can fix, please go ahead and contribute a pull request (PR). You don't need to ask permission.

The Loki documentation is written using the CommonMark flavor of markdown, including some extended features. For more information about markdown, you can see the [CommonMark specification](https://spec.commonmark.org/), and a [quick reference guide](https://commonmark.org/help/) for CommonMark.

If you have a GitHub account and you're just making a small fix, for example fixing a typo or updating an example, you can edit the topic in GitHub.

1. Find the topic in the Loki repo.
1. Click the pencil icon.
1. Enter your changes.
1. Click **Commit changes**. GitHub creates a pull request for you.
1. If this is your first contribution to the Loki repository, you will need to sign the Contributor License Agreement (CLA) before your PR can be accepted.
1. Add the `type/docs` label to identify your PR as a docs contribution.
1. If your contribution needs to be added to the current release or previous releases, apply the appropriate `backport` label.  You can find more information about backporting in the [Writers' toolkit](https://grafana.com/docs/writers-toolkit/review/backporting/).

For larger contributions, for example documenting a new feature or adding a new topic, consider running the project locally to see how the changes look like before making a pull request.

The docs team has created a [Writer's Toolkit](https://grafana.com/docs/writers-toolkit/) that documents how we write documentation at Grafana Labs. The Writer's toolkit contains information about how we structure documentation at Grafana, including templates for different types of topics, information about Hugo shortcodes that extend markdown to add additional features, and information about linters and other tools that we use to write documentation. The Writers' Toolkit also includes our [Style Guide](https://grafana.com/docs/writers-toolkit/write/style-guide/).

Note that in Hugo the structure of the documentation is based on the folder structure of the documentation repository. The URL structure is generated based on the folder structure and file names. Try to avoid moving or renaming files, as this will break cross-references to those files. If you must move or rename files, run `make docs` as described below to find and fix broken links before you submit your pull request.

## Testing documentation

Loki uses the static site generator [Hugo](https://gohugo.io/) to generate the documentation. The Loki repository uses a continuous integration (CI) action to sync documentation to the [Grafana website](https://grafana.com/docs/loki/latest). The CI is triggered on every merge to main in the `docs` subfolder.

You can preview the documentation in GitHub, but GitHub does not render images or any of the Hugo shortcodes. However, you can preview the documentation locally after installing [Docker](https://www.docker.com/) or [Podman](https://podman.io/).

To get a local preview of the documentation:
1. Run Docker (or Podman).
2. Navigate to the directory with the documentation makefile, `/loki/docs`.
3. Run the command `make docs`. This uses the `grafana/docs` image which internally uses Hugo to generate the static site.
4. Open http://localhost:3002/docs/loki/latest/ to review your changes.

> Note that `make docs` uses a lot of memory. If it crashes, increase the memory allocated to Docker and try again.

For more information about building and testing documentation, see [build and review](https://grafana.com/docs/writers-toolkit/review/) section of the Writers' Toolkit
