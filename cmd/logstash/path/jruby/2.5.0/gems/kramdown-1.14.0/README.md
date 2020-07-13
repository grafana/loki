# kramdown

## Readme first!

kramdown was originally licensed under the GPL until the 1.0.0 release. However, due to the many
requests it is now released under the MIT license and therefore can easily be used in commercial
projects, too.

However, if you use kramdown in a commercial setting, please consider **contributing back any
changes** for the benefit of the community and/or
[**becoming a patron**](https://www.patreon.com/gettalong) - thanks!


## Introduction

kramdown is a fast, pure Ruby Markdown superset converter, using a strict syntax definition and
supporting several common extensions.

The syntax definition for the kramdown syntax can be found in **doc/syntax.page** (or online at
<http://kramdown.gettalong.org/syntax.html>) and a quick reference is available in
**doc/quickref.page** or online at <http://kramdown.gettalong.org/quickref.html>.

The kramdown library is mainly written to support the kramdown-to-HTML conversion chain. However,
due to its flexibility (by creating an internal AST) it supports other input and output formats as
well. Here is a list of the supported formats:

* input formats: kramdown (a Markdown superset), Markdown, GFM, HTML
* output formats: HTML, kramdown, LaTeX (and therefore PDF), PDF via Prawn

All the documentation on the available input and output formats is available in the **doc/**
directory and online at <http://kramdown.gettalong.org>.

Starting from version 1.0.0 kramdown is using a versioning scheme with major, minor and patch parts
in the version number where the major number changes on backwards-incompatible changes, the minor
number on the introduction of new features and the patch number on everything else.

For information about changes between versions, have a look at
<http://kramdown.gettalong.org/news.html> or the commit history!


## Usage

kramdown has a very simple API, so using kramdown is as easy as

```ruby
require 'kramdown'

Kramdown::Document.new(text).to_html
```

For detailed information have a look at the API documentation of the `Kramdown::Document` class.

The full API documentation is available at <http://kramdown.gettalong.org/rdoc/>, other sites with an
API documentation for kramdown probably don't provide the complete documentation!

There are also some third-party libraries that extend the functionality of kramdown -- see the
kramdown Wiki at <https://github.com/gettalong/kramdown/wiki>.


## Development

Just clone the git repository as described in **doc/installation.page** and you are good to go. You
probably want to install `rake` so that you can use the provided rake tasks.

If you want to run the tests, the development dependencies are needed as well as some additional
programs like `tidy` and `latex`. See the `.travis.yml` file for more information.


## License

MIT - see the **COPYING** file.
