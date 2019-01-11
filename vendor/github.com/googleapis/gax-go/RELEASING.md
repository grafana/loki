# Releasing

1. Make changes.
2. Determine next version. For the sake of this list, let's assume 1.2.3.
3. Update `gax.go` with `const Version = "1.2.3"`. If v2, do the same to `v2/gax.go`.
4. `git checkout -b my_branch && git commit && git push my_remote my_branch`
5. Get it reviewed and merged.
6. `git checkout master && git tag v1.2.3 && git push origin v1.2.3`
7. Update the [releases page on GitHub](https://github.com/googleapis/gax-go/releases)
