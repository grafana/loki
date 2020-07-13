if ENV['COVERAGE']
  require 'simplecov'
  require 'coveralls'

  SimpleCov.formatter = SimpleCov::Formatter::MultiFormatter[
    SimpleCov::Formatter::HTMLFormatter,
    Coveralls::SimpleCov::Formatter
  ]
  SimpleCov.start do
    add_filter 'spec/'
    add_filter 'vendor/'
  end
end

require "logstash-core"
require "logstash/logging"
require "logstash/devutils/rspec/logstash_helpers"

Thread.abort_on_exception = true

# set log4j configuration
unless java.lang.System.getProperty("log4j.configurationFile")
  log4j2_properties = "#{File.dirname(__FILE__)}/log4j2.properties"
  LogStash::Logging::Logger::initialize("file:///" + log4j2_properties)
end

$TESTING = true
if RUBY_VERSION < "2.3"
  raise LoadError.new("Ruby >= 2.3.0 or later is required. (You are running: " + RUBY_VERSION + ")")
end

if level = (ENV["TEST_DEBUG"] || ENV['LOGGER_LEVEL'])
  logger, level = level.split('=') # 'logstash.filters.grok=DEBUG'
  level, logger = logger, nil if level.nil? # only level given e.g. 'DEBUG'
  level = org.apache.logging.log4j.Level.toLevel(level, org.apache.logging.log4j.Level::WARN)
  LogStash::Logging::Logger::configure_logging(level.to_s, logger)
else
  LogStash::Logging::Logger::configure_logging('ERROR')
end

RSpec.configure do |config|
  # for now both include and extend are required because the newly refactored "input" helper method need to be visible in a "it" block
  # and this is only possible by calling include on LogStashHelper
  config.include LogStashHelper
  config.extend LogStashHelper

  config.filter_run_excluding LogStashHelper.excluded_tags

  config.around(:each) do |example|
    @__current_example_metadata = example.metadata
    example.run
  end

  # Run specs in random order to surface order dependencies. If you find an
  # order dependency and want to debug it, you can fix the order by providing
  # the seed, which is printed after each run.
  #     --seed 1234
  config.order = :random
end

