# Stud.

Ruby's stdlib is missing many things I use to solve most of my software
problems. Things like like retrying on a failure, supervising workers, resource
pools, etc.

In general, I started exploring solutions to these things in code over in my
[software-patterns](https://github.com/jordansissel/software-patterns) repo.
This library (stud) aims to be a well-tested, production-quality implementation
of the patterns described in that repo.

For now, these all exist in a single repo because, so far, implementations of
each 'pattern' are quite small by code size.

## Features

* {Stud::Try} (and {Stud.try}) - retry on failure, with back-off, where failure is any exception.
* {Stud::Pool} - generic resource pools
* {Stud::Task} - tasks (threads that can return values, exceptions, etc)
* {Stud.interval} - interval execution (do X every N seconds)
* {Stud::Buffer} - batch & flush behavior.

## TODO:

* Make sure all things are documented. rubydoc.info should be able to clearly
  show folks how to use features of this library.
* Add tests to cover all supported features.
