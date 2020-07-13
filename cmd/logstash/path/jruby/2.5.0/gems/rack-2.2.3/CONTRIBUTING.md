Contributing to Rack
=====================

Rack is work of [hundreds of contributors](https://github.com/rack/rack/graphs/contributors). You're encouraged to submit [pull requests](https://github.com/rack/rack/pulls), [propose features and discuss issues](https://github.com/rack/rack/issues). When in doubt, post to the [rack-devel](http://groups.google.com/group/rack-devel) mailing list.

#### Fork the Project

Fork the [project on Github](https://github.com/rack/rack) and check out your copy.

```
git clone https://github.com/contributor/rack.git
cd rack
git remote add upstream https://github.com/rack/rack.git
```

#### Create a Topic Branch

Make sure your fork is up-to-date and create a topic branch for your feature or bug fix.

```
git checkout master
git pull upstream master
git checkout -b my-feature-branch
```

#### Bundle Install and Quick Test

Ensure that you can build the project and run quick tests.

```
bundle install --without extra
bundle exec rake test
```

#### Running All Tests

Install all dependencies.

```
bundle install
```

Run all tests.

```
rake test
```

The test suite has no dependencies outside of the core Ruby installation and bacon.

Some tests will be skipped if a dependency is not found.

To run the test suite completely, you need:

  * fcgi
  * dalli
  * thin

To test Memcache sessions, you need memcached (will be run on port 11211) and dalli installed.

#### Write Tests

Try to write a test that reproduces the problem you're trying to fix or describes a feature that you want to build.

We definitely appreciate pull requests that highlight or reproduce a problem, even without a fix.

#### Write Code

Implement your feature or bug fix.

Make sure that `bundle exec rake fulltest` completes without errors.

#### Write Documentation

Document any external behavior in the [README](README.rdoc).

#### Update Changelog

Add a line to [CHANGELOG](CHANGELOG.md).

#### Commit Changes

Make sure git knows your name and email address:

```
git config --global user.name "Your Name"
git config --global user.email "contributor@example.com"
```

Writing good commit logs is important. A commit log should describe what changed and why.

```
git add ...
git commit
```

#### Push

```
git push origin my-feature-branch
```

#### Make a Pull Request

Go to https://github.com/contributor/rack and select your feature branch. Click the 'Pull Request' button and fill out the form. Pull requests are usually reviewed within a few days.

#### Rebase

If you've been working on a change for a while, rebase with upstream/master.

```
git fetch upstream
git rebase upstream/master
git push origin my-feature-branch -f
```

#### Make Required Changes

Amend your previous commit and force push the changes.

```
git commit --amend
git push origin my-feature-branch -f
```

#### Check on Your Pull Request

Go back to your pull request after a few minutes and see whether it passed muster with Travis-CI. Everything should look green, otherwise fix issues and amend your commit as described above.

#### Be Patient

It's likely that your change will not be merged and that the nitpicky maintainers will ask you to do more, or fix seemingly benign problems. Hang on there!

#### Thank You

Please do know that we really appreciate and value your time and work. We love you, really.
