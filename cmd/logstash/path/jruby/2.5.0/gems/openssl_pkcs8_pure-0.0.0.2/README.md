# openssl_pkcs8_pure
- Add `OpenSSL::PKey::DSA#to_pem_pkcs8`, `OpenSSL::PKey::RSA#to_pem_pkcs8`, `OpenSSL::PKey::EC#to_pem_pkcs8`.
- There used to be an attempt [openssl_pkcs8](https://github.com/twg/openssl_pkcs8), but it was over in incomplete form...
  - This openssl_pkcs8_pure is written in Ruby (I say "pure"), so it will not have incompatibility...

[![Build Status](https://travis-ci.org/cielavenir/openssl_pkcs8_pure.png)](https://travis-ci.org/cielavenir/openssl_pkcs8_pure) [![Code Climate](https://codeclimate.com/github/cielavenir/openssl_pkcs8_pure.png)](https://codeclimate.com/github/cielavenir/openssl_pkcs8_pure) [![Coverage Status](https://coveralls.io/repos/cielavenir/openssl_pkcs8_pure/badge.png)](https://coveralls.io/r/cielavenir/openssl_pkcs8_pure)

## Supported Ruby versions
* Ruby 1.8.7 or later
* rubinius
* jruby (RSA only)

## Caveats
* Passphrase is not supported yet.
* On Ruby 1.8, the output PEM's Base64 folding is different from original openssl.
* This implementation is built from my own research, so it might be unstable (of course, to avoid unstability we have rspec)

## Binary distribution
* https://rubygems.org/gems/openssl_pkcs8_pure

## Install
* gem install openssl_pkcs8_pure

## Usage
* Please check spec/openssl_pkcs8_pure.spec as an example.
* Also you can refer to [ctouch/zip.rb](https://github.com/cielavenir/ctouch/blob/master/support/zip.rb) (the first motivation is that Chrome WebStore only accepts PKCS8 key).

## Contributing to openssl_pkcs8_pure
* Check out the latest master to make sure the feature hasn't been implemented or the bug hasn't been fixed yet.
* Check out the issue tracker to make sure someone already hasn't requested it and/or contributed it.
* Fork the project.
* Start a feature/bugfix branch.
* Commit and push until you are happy with your contribution.
* Make sure to add tests for it. This is important so I don't break it in a future version unintentionally.
* Please try not to mess with the Rakefile, version, or history. If you want to have your own version, or is otherwise necessary, that is fine, but please isolate to its own commit so I can cherry-pick around it.

## Copyright
Copyright (c) 2017 T. Yamada under Ruby License (2-clause BSDL or Artistic).
See LICENSE.txt for further details.
