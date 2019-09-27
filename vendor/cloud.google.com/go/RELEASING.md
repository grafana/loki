# How to release `cloud.google.com/go`

1. Determine the current release version with `git tag -l`. It should look
   something like `vX.Y.Z`. We'll call the current version `$CV` and the new
   version `$NV`.
1. On master, run `git log $CV...` to list all the changes since the last
   release. NOTE: You must manually exclude changes from submodules [1].
1. Edit `CHANGES.md` to include a summary of the changes.
1. `cd internal/version && go generate && cd -`
1. `./tidyall.sh`
1. Mail the CL. When the CL is approved, submit it.
1. Without submitting any other CLs:
   a. Switch to master.
   b. `git pull`
   c. Tag the repo with the next version: `git tag $NV`.
   d. Push the tag: `git push origin $NV`.
1. Update [the releases page](https://github.com/googleapis/google-cloud-go/releases)
   with the new release, copying the contents of `CHANGES.md`.

# How to release a submodule

We have several submodules, including cloud.google.com/go/logging,
cloud.google.com/go/datastore, and so on.

To release a submodule:

1. (these instructions assume we're releasing cloud.google.com/go/datastore -
   adjust accordingly)
1. Determine the current release version with `git tag -l`. It should look
   something like `datastore/vX.Y.Z`. We'll call the current version `$CV` and
   the new version `$NV`, which should look something like `datastore/vX.Y+1.Z`
   (assuming a minor bump).
1. On master, run `git log $CV.. -- datastore/` to list all the changes to the
   submodule directory since the last release.
1. Edit `datastore/CHANGES.md` to include a summary of the changes.
1. `./tidyall.sh`
1. `cd internal/version && go generate && cd -`
1. Mail the CL. When the CL is approved, submit it.
1. Without submitting any other CLs:
   a. Switch to master.
   b. `git pull`
   c. Tag the repo with the next version: `git tag $NV`.
   d. Push the tag: `git push origin $NV`.
1. Update [the releases page](https://github.com/googleapis/google-cloud-go/releases)
   with the new release, copying the contents of `datastore/CHANGES.md`.

# Appendix

1: This should get better as submodule tooling matures.
