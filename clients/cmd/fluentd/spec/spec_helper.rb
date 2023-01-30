# frozen_string_literal: true

require 'rubocop'
require 'rubocop/rspec/support'

require 'bundler/setup'
require 'fluent/plugin/out_loki'
require 'simplecov'

SimpleCov.start
puts 'SimpleCov started successfully!'

spec_helper_glob = File.expand_path('{support,shared}/*.rb', __dir__)
Dir.glob(spec_helper_glob).map(&method(:require))

RSpec.configure do |config|
  # Enable flags like --only-failures and --next-failure
  config.example_status_persistence_file_path = '.rspec_status'

  # Disable RSpec exposing methods globally on `Module` and `main`
  config.disable_monkey_patching!

  # We should address configuration warnings when we upgrade
  config.raise_errors_for_deprecations!

  # RSpec gives helpful warnings when you are doing something wrong.
  # We should take their advice!
  config.raise_on_warning = true

  config.expect_with :rspec do |c|
    c.syntax = :expect
  end
end
require 'rubocop-rspec'
