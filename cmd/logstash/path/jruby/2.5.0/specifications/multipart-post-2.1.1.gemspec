# -*- encoding: utf-8 -*-
# stub: multipart-post 2.1.1 ruby lib

Gem::Specification.new do |s|
  s.name = "multipart-post".freeze
  s.version = "2.1.1"

  s.required_rubygems_version = Gem::Requirement.new(">= 0".freeze) if s.respond_to? :required_rubygems_version=
  s.require_paths = ["lib".freeze]
  s.authors = ["Nick Sieger".freeze, "Samuel Williams".freeze]
  s.date = "2019-05-13"
  s.description = "Use with Net::HTTP to do multipart form postspec. IO values that have #content_type, #original_filename, and #local_path will be posted as a binary file.".freeze
  s.email = ["nick@nicksieger.com".freeze, "samuel.williams@oriontransfer.co.nz".freeze]
  s.homepage = "https://github.com/nicksieger/multipart-post".freeze
  s.licenses = ["MIT".freeze]
  s.rubygems_version = "3.0.6".freeze
  s.summary = "A multipart form post accessory for Net::HTTP.".freeze

  s.installed_by_version = "3.0.6" if s.respond_to? :installed_by_version

  if s.respond_to? :specification_version then
    s.specification_version = 4

    if Gem::Version.new(Gem::VERSION) >= Gem::Version.new('1.2.0') then
      s.add_development_dependency(%q<bundler>.freeze, [">= 1.3", "< 3"])
      s.add_development_dependency(%q<rspec>.freeze, ["~> 3.4"])
      s.add_development_dependency(%q<rake>.freeze, [">= 0"])
    else
      s.add_dependency(%q<bundler>.freeze, [">= 1.3", "< 3"])
      s.add_dependency(%q<rspec>.freeze, ["~> 3.4"])
      s.add_dependency(%q<rake>.freeze, [">= 0"])
    end
  else
    s.add_dependency(%q<bundler>.freeze, [">= 1.3", "< 3"])
    s.add_dependency(%q<rspec>.freeze, ["~> 3.4"])
    s.add_dependency(%q<rake>.freeze, [">= 0"])
  end
end
