# Use of vendored code in Weave Cortex

Weave Cortex is licensed under the [Apache 2.0 license](LICENSE).
Some vendored code is under different licenses though, all of them ship the
entire license text they are under.

The following bits of vendored code are under the MPL-2.0 and can be found
in the ./vendor/ directory:

- https://github.com/hashicorp/go-cleanhttp and  
  https://github.com/hashicorp/consul
- Pulled in by dependencies are  
  https://github.com/hashicorp/golang-lru  
  https://github.com/hashicorp/serf  
  https://github.com/hashicorp/go-rootcerts

[One file used in tests](COPYING.LGPL-3) is under LGPL-3, that's why we ship
the license text in this repository.
