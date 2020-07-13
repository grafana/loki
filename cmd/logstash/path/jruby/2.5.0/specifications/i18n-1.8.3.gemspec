# -*- encoding: utf-8 -*-
# stub: i18n 1.8.3 ruby lib

Gem::Specification.new do |s|
  s.name = "i18n".freeze
  s.version = "1.8.3"

  s.required_rubygems_version = Gem::Requirement.new(">= 1.3.5".freeze) if s.respond_to? :required_rubygems_version=
  s.metadata = { "bug_tracker_uri" => "https://github.com/svenfuchs/i18n/issues", "changelog_uri" => "https://github.com/svenfuchs/i18n/releases", "documentation_uri" => "https://guides.rubyonrails.org/i18n.html", "source_code_uri" => "https://github.com/svenfuchs/i18n" } if s.respond_to? :metadata=
  s.require_paths = ["lib".freeze]
  s.authors = ["Sven Fuchs".freeze, "Joshua Harvey".freeze, "Matt Aimonetti".freeze, "Stephan Soller".freeze, "Saimon Moore".freeze, "Ryan Bigg".freeze]
  s.date = "2020-06-05"
  s.description = "New wave Internationalization support for Ruby.".freeze
  s.email = "rails-i18n@googlegroups.com".freeze
  s.homepage = "https://github.com/ruby-i18n/i18n".freeze
  s.licenses = ["MIT".freeze]
  s.post_install_message = "\nHEADS UP! i18n 1.1 changed fallbacks to exclude default locale.\nBut that may break your application.\n\nIf you are upgrading your Rails application from an older version of Rails:\n\nPlease check your Rails app for 'config.i18n.fallbacks = true'.\nIf you're using I18n (>= 1.1.0) and Rails (< 5.2.2), this should be\n'config.i18n.fallbacks = [I18n.default_locale]'.\nIf not, fallbacks will be broken in your app by I18n 1.1.x.\n\nIf you are starting a NEW Rails application, you can ignore this notice.\n\nFor more info see:\nhttps://github.com/svenfuchs/i18n/releases/tag/v1.1.0\n\n".freeze
  s.required_ruby_version = Gem::Requirement.new(">= 2.3.0".freeze)
  s.rubygems_version = "3.0.6".freeze
  s.summary = "New wave Internationalization support for Ruby".freeze

  s.installed_by_version = "3.0.6" if s.respond_to? :installed_by_version

  if s.respond_to? :specification_version then
    s.specification_version = 4

    if Gem::Version.new(Gem::VERSION) >= Gem::Version.new('1.2.0') then
      s.add_runtime_dependency(%q<concurrent-ruby>.freeze, ["~> 1.0"])
    else
      s.add_dependency(%q<concurrent-ruby>.freeze, ["~> 1.0"])
    end
  else
    s.add_dependency(%q<concurrent-ruby>.freeze, ["~> 1.0"])
  end
end
