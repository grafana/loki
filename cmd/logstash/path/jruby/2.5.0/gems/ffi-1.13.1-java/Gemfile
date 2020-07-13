source 'https://rubygems.org'

group :development do
  gem 'rake', '~> 13.0'
  gem 'rake-compiler', '~> 1.0.3'
  gem 'rake-compiler-dock', '~> 1.0'
  gem 'rspec', '~> 3.0'
  # irb is a dependency of rubygems-tasks 0.2.5.
  # irb versions > 1.1.1 depend on reline,
  # which sometimes causes 'bundle install' to fail on Ruby <= 2.4: https://github.com/rubygems/rubygems/issues/3463
  gem 'rubygems-tasks', '>= 0.2', '< 0.2.5', :require => 'rubygems/tasks'
end

group :doc do
  gem 'kramdown'
  gem 'yard', '~> 0.9'
end
