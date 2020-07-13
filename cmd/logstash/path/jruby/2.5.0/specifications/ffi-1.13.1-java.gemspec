# -*- encoding: utf-8 -*-
# stub: ffi 1.13.1 java lib

Gem::Specification.new do |s|
  s.name = "ffi".freeze
  s.version = "1.13.1"
  s.platform = "java".freeze

  s.required_rubygems_version = Gem::Requirement.new(">= 0".freeze) if s.respond_to? :required_rubygems_version=
  s.metadata = { "bug_tracker_uri" => "https://github.com/ffi/ffi/issues", "changelog_uri" => "https://github.com/ffi/ffi/blob/master/CHANGELOG.md", "documentation_uri" => "https://github.com/ffi/ffi/wiki", "mailing_list_uri" => "http://groups.google.com/group/ruby-ffi", "source_code_uri" => "https://github.com/ffi/ffi/", "wiki_uri" => "https://github.com/ffi/ffi/wiki" } if s.respond_to? :metadata=
  s.require_paths = ["lib".freeze]
  s.authors = ["Wayne Meissner".freeze]
  s.cert_chain = ["-----BEGIN CERTIFICATE-----\nMIIDPDCCAiSgAwIBAgIBATANBgkqhkiG9w0BAQsFADAkMSIwIAYDVQQDDBl0cmF2\naXMtY2kvREM9ZHVtbXkvREM9b3JnMB4XDTIwMDQwODExMDA0MFoXDTIxMDQwODEx\nMDA0MFowJDEiMCAGA1UEAwwZdHJhdmlzLWNpL0RDPWR1bW15L0RDPW9yZzCCASIw\nDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAK+5uh+zqCXKho0zXIaLmJD6YDpa\nl07nJ+PQFcMBYgsKA2ip01THj3DVYwP/va6hYgqPmxEJB3tsEthKnHVHm0dgqqg/\ngfyDFU0ZfbSYKeNlZQRIdddKPc6dNbmtY2gBWFt6YOZnBccsgJmSUAbh0a9xhVbm\nqAStn/q7eq9iW9+12AB9HM+QCWrsCAXEHGGNENDAK9HtHwBs4KsneiIQY5rd/Mzs\nIi25XXnDUa1NjC4u/mMuJXBpWLw2rEAQkzEFQBZR0W0ehm9Mi4TokhLy/QH8GRaH\n0KADzpk1cxuOrEBIhy6ISQs7g/tI6YTePAmDMTsodov02FZCcMpoxOifpFkCAwEA\nAaN5MHcwCQYDVR0TBAIwADALBgNVHQ8EBAMCBLAwHQYDVR0OBBYEFOTn5ek+QtcI\ng+ZglIvRPAPGoXvLMB4GA1UdEQQXMBWBE3RyYXZpcy1jaUBkdW1teS5vcmcwHgYD\nVR0SBBcwFYETdHJhdmlzLWNpQGR1bW15Lm9yZzANBgkqhkiG9w0BAQsFAAOCAQEA\nXuaj6r4QXrQwDdrmT2sPgWiS5/CRRiSwyWsE1jfM0q5spzuKRIy+vSfmj6isScGf\nvT7nwXus+3IcISSdfUddGqf/L54z6U9dVc+V5SH+QuptDRPgQ+fPJFKn8uFARJDU\n9qNNSKpyoXXHksouuRV4dXVYRChfhLiavXaR0jNmi6qgTsSSvyKD2aObyVdVUzxr\nUmpavc3v5BbquwF9DlKeHZwU/qP0ynCZg/Z9CFa18e5JfyBSFXHXDA0YXE6+b4Dh\n+QvsIa+rEKMYbn0IActnL2SCNlGcttHDcH0AkctmwmAN2r6LTInUBCJoahaIeIXU\nRE1H1QyCW3SV93CTTKaR2A==\n-----END CERTIFICATE-----\n".freeze]
  s.date = "2020-06-09"
  s.description = "Ruby FFI library".freeze
  s.email = "wmeissner@gmail.com".freeze
  s.homepage = "https://github.com/ffi/ffi/wiki".freeze
  s.licenses = ["BSD-3-Clause".freeze]
  s.rdoc_options = ["--exclude=ext/ffi_c/.*\\.o$".freeze, "--exclude=ffi_c\\.(bundle|so)$".freeze]
  s.required_ruby_version = Gem::Requirement.new(">= 2.3".freeze)
  s.rubygems_version = "3.0.6".freeze
  s.summary = "Ruby FFI".freeze

  s.installed_by_version = "3.0.6" if s.respond_to? :installed_by_version

  if s.respond_to? :specification_version then
    s.specification_version = 4

    if Gem::Version.new(Gem::VERSION) >= Gem::Version.new('1.2.0') then
      s.add_development_dependency(%q<rake>.freeze, ["~> 13.0"])
      s.add_development_dependency(%q<rake-compiler>.freeze, ["~> 1.0"])
      s.add_development_dependency(%q<rake-compiler-dock>.freeze, ["~> 1.0"])
      s.add_development_dependency(%q<rspec>.freeze, ["~> 2.14.1"])
      s.add_development_dependency(%q<rubygems-tasks>.freeze, ["~> 0.2.4"])
    else
      s.add_dependency(%q<rake>.freeze, ["~> 13.0"])
      s.add_dependency(%q<rake-compiler>.freeze, ["~> 1.0"])
      s.add_dependency(%q<rake-compiler-dock>.freeze, ["~> 1.0"])
      s.add_dependency(%q<rspec>.freeze, ["~> 2.14.1"])
      s.add_dependency(%q<rubygems-tasks>.freeze, ["~> 0.2.4"])
    end
  else
    s.add_dependency(%q<rake>.freeze, ["~> 13.0"])
    s.add_dependency(%q<rake-compiler>.freeze, ["~> 1.0"])
    s.add_dependency(%q<rake-compiler-dock>.freeze, ["~> 1.0"])
    s.add_dependency(%q<rspec>.freeze, ["~> 2.14.1"])
    s.add_dependency(%q<rubygems-tasks>.freeze, ["~> 0.2.4"])
  end
end
