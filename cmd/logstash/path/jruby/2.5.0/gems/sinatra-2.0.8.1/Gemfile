# Why use bundler?
# Well, not all development dependencies install on all rubies. Moreover, `gem
# install sinatra --development` doesn't work, as it will also try to install
# development dependencies of our dependencies, and those are not conflict free.
# So, here we are, `bundle install`.
#
# If you have issues with a gem: `bundle install --without-coffee-script`.

RUBY_ENGINE = 'ruby' unless defined? RUBY_ENGINE
source 'https://rubygems.org' unless ENV['QUICK']
gemspec

gem 'rake'
gem 'rack', git: 'https://github.com/rack/rack.git'
gem 'rack-test', '>= 0.6.2'
gem "minitest", "~> 5.0"
gem 'yard'

gem "rack-protection", path: "rack-protection"
gem "sinatra-contrib", path: "sinatra-contrib"

gem "twitter-text", "1.14.7"

if RUBY_ENGINE == 'jruby'
  gem 'nokogiri', '!= 1.5.0'
  gem 'trinidad'
end

if RUBY_ENGINE == 'jruby' || RUBY_ENGINE == 'ruby'
  gem "activesupport", "~> 5.1.6"
end

if RUBY_ENGINE == "ruby"
  gem 'less', '~> 2.0'
  gem 'therubyracer'
  gem 'redcarpet'
  gem 'wlang', '>= 2.0.1'
  gem 'bluecloth'
  gem 'rdiscount'
  gem 'RedCloth'
  gem 'puma'
  gem 'yajl-ruby'
  gem 'nokogiri'
  gem 'thin'
  gem 'slim', '~> 2.0'
  gem 'coffee-script', '>= 2.0'
  gem 'rdoc'
  gem 'kramdown'
  gem 'maruku'
  gem 'creole'
  gem 'wikicloth'
  gem 'markaby'
  gem 'radius'
  gem 'asciidoctor'
  gem 'liquid'
  gem 'stylus'
  gem 'rabl'
  gem 'builder'
  gem 'erubi'
  gem 'erubis'
  gem 'haml', '>= 3.0'
  gem 'sass'
  gem 'reel-rack'
  gem 'celluloid', '~> 0.16.0'
  gem 'commonmarker', '~> 0.20.0'
  gem 'pandoc-ruby', '~> 2.0.2'
  gem 'simplecov', require: false
end

if RUBY_ENGINE == "rbx"
  gem 'json'
  gem 'rubysl'
  gem 'rubysl-test-unit'
  gem 'erubi'
end

platforms :jruby do
  gem 'json'
end
