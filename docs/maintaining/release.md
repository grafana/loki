# Releasing Loki

## How To Perform a Release

* Create a new branch for updating changelog and version numbers
    * In the changelog, set the version number and release date, create the next release as (unreleased) as a placeholder for people to add notes to the changelog
    * List all the merged PR's since the last release, this command is helpful for generating the output: `curl https://api.github.com/search/issues?q=repo:grafana/loki+is:pr+"merged:>=2019-08-02" | jq -r ' .items[] | "* [" + (.number|tostring) + "](" + .html_url + ") **" + .user.login + "**: " + .title'`
* Go through the `docs/` and update references to the previous release version to the new one.

> Until [852](https://github.com/grafana/loki/issues/852) is fixed, updating the Helm and Ksonnet configs has to wait until after the release tag is pushed so that the Helm tests will pass.

* Merge the changelog PR
* Run:

    **Note: This step creates the tag and therefore the release, this will trigger CI to build release artifacts (binaries and images) as well as publish them.  As soon as this tag is pushed when CI finishes the new release artifacts will be available to the public**

```https://github.com/grafana/loki/releases
git pull
git tag -s v0.2.0 -m "tagging release v0.2.0"
git push origin v0.2.0
```

* After the builds finish, run:

```
make release-prepare
```

* Set the release version and in most cases the auto selected helm version numbers should be fine.
* Commit to another branch, make a PR and get merge it.
* Go to GitHub and finish manually editing the auto generated release template and publish it!



