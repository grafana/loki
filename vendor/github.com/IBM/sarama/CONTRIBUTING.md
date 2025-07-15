# Contributing

[fork]: https://github.com/IBM/sarama/fork
[pr]: https://github.com/IBM/sarama/compare
[released]: https://help.github.com/articles/github-terms-of-service/#6-contributions-under-repository-license

Hi there! We are thrilled that you would like to contribute to Sarama.
Contributions are always welcome, both reporting issues and submitting pull requests!

## Reporting issues

Please make sure to include any potentially useful information in the issue, so we can pinpoint the issue faster without going back and forth.

- What SHA of Sarama are you running? If this is not the latest SHA on the main branch, please try if the problem persists with the latest version.
- You can set `sarama.Logger` to a [log.Logger](http://golang.org/pkg/log/#Logger) instance to capture debug output. Please include it in your issue description.
- Also look at the logs of the Kafka broker you are connected to. If you see anything out of the ordinary, please include it.

Also, please include the following information about your environment, so we can help you faster:

- What version of Kafka are you using?
- What version of Go are you using?
- What are the values of your Producer/Consumer/Client configuration?


## Contributing a change

Contributions to this project are [released][released] to the public under the project's [opensource license](LICENSE.md).
By contributing to this project you agree to the [Developer Certificate of Origin](https://developercertificate.org/) (DCO).
The DCO was created by the Linux Kernel community and is a simple statement that you, as a contributor, wrote or otherwise have the legal right to contribute those changes.

Contributors must _sign-off_ that they adhere to these requirements by adding a `Signed-off-by` line to all commit messages with an email address that matches the commit author:

```
feat: this is my commit message

Signed-off-by: Random J Developer <random@developer.example.org>
```

Git even has a `-s` command line option to append this automatically to your
commit message:

```
$ git commit -s -m 'This is my commit message'
```

Because this library is in production use by many people and applications, we code review all additions.
To make the review process go as smooth as possible, please consider the following.

- If you plan to work on something major, please open an issue to discuss the design first.
- Don't break backwards compatibility. If you really have to, open an issue to discuss this first.
- Make sure to use the `go fmt` command to format your code according to the standards. Even better, set up your editor to do this for you when saving.
- Run [go vet](https://golang.org/cmd/vet/) to detect any suspicious constructs in your code that could be bugs.
- Explicitly handle all error return values. If you really want to ignore an error value, you can assign it to `_`. You can use [errcheck](https://github.com/kisielk/errcheck) to verify whether you have handled all errors.
- You may also want to run [golint](https://github.com/golang/lint) as well to detect style problems.
- Add tests that cover the changes you made. Make sure to run `go test` with the `-race` argument to test for race conditions.
- Make sure your code is supported by all the Go versions we support.
  You can rely on GitHub Actions for testing older Go versions.

## Submitting a pull request

0. [Fork][fork] and clone the repository
1. Create a new branch: `git checkout -b my-branch-name`
2. Make your change, push to your fork and [submit a pull request][pr]
3. Wait for your pull request to be reviewed and merged.

Here are a few things you can do that will increase the likelihood of your pull request being accepted:

- Keep your change as focused as possible. If there are multiple changes you would like to make that are not dependent upon each other, consider submitting them as separate pull requests.
- Write a [good commit message](http://tbaggery.com/2008/04/19/a-note-about-git-commit-messages.html).

## Further Reading

- [Developer Certificate of Origin versus Contributor License Agreements](https://julien.ponge.org/blog/developer-certificate-of-origin-versus-contributor-license-agreements/)
- [The most powerful contributor agreement](https://lwn.net/Articles/592503/)
- [How to Contribute to Open Source](https://opensource.guide/how-to-contribute/)
- [Using Pull Requests](https://help.github.com/articles/about-pull-requests/)
- [GitHub Help](https://help.github.com)
