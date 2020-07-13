# Changelog

## 0.6.5 (2015-05-02)

* Catch signals and exit appropriately.

## 0.6.4 (2015-02-26)

* Ensure computed defaults are only computed once.

## 0.6.3 (2013-11-14)

* Specify (MIT) license.

## 0.6.2 (2013-11-06)

* Refactoring around multi-valued attributes.
* Allow injection of a custom help-builder.

## 0.6.1 (2013-05-07)

* Signal a usage error when an environment_variable fails validation.
* Refactor setting, defaulting and inheritance of attributes.

## 0.6.0 (2013-04-28)

* Introduce "banner" to describe a command (replacing "self.description=").
* Introduce "Clamp do ... end" syntax sugar.
* Allow parameters to be specified before a subcommand.
* Add support for :multivalued options.
* Multi valued options and parameters get an "#append_to_foo_list" method, rather than
  "#foo_list=".
* default_subcommand must be specified before any subcommands.
