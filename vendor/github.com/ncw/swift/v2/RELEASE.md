# How to make a release

Check master is building properly

Check on master

    git checkout master

Tag

    git tag -s v2.0.x -m "Release v2.0.x"

Push the signed tag

    git push --follow-tags

Go to https://github.com/ncw/swift/tags and check the tag is there.

From there click create a release.

Use the generate release notes button and publish.

Possibly use this instead?

    gh release create v2.0.x --generate-notes
