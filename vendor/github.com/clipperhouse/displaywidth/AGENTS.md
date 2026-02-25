The goals and overview of this package can be found in the README.md file,
start by reading that.

The goal of this package is to determine the display (column) width of a
string, UTF-8 bytes, or runes, as would happen in a monospace font, especially
in a terminal.

When troubleshooting, write Go unit tests instead of executing debug scripts.
The tests can return whatever logs or output you need. If those tests are
only for temporary troubleshooting, clean up the tests after the debugging is
done.

(Separate executable debugging scripts are messy, tend to have conflicting
dependencies and are hard to cleanup.)

If you make changes to the trie generation in internal/gen, it can be invoked
by running `go generate` from the top package directory.

## Pull Requests and branches

For PRs (pull requests), you can use the gh CLI tool to retrieve details,
or post comments. Then, compare the current branch with main. Reviewing a PR
and reviewing a branch are about the same, but the PR may add context.

Look for bugs. Think like GitHub Copilot or Cursor BugBot.

Offer to post a brief summary of the review to the PR, via the gh CLI tool.

## Comparisons to go-runewidth

We originally attempted to make this package compatible with go-runewidth.
However, we found that there were too many differences in the handling of
certain characters and properties.

We believe, preliminarily, that our choices are more correct and complete,
by using more complete categories such as Unicode Cf (format) for zero-width
and Mn (Nonspacing_Mark) for combining marks.
