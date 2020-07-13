require "rspec"
require "rspec/wait/error"
require "rspec/wait/handler"
require "rspec/wait/proxy"
require "rspec/wait/target"

module RSpec
  module Wait
    module_function

    # From: https://github.com/rspec/rspec-expectations/blob/v3.0.0/lib/rspec/expectations/syntax.rb#L72-L74
    def wait_for(value = Target::UndefinedValue, &block)
      Target.for(value, block)
    end

    def wait(timeout = nil, options = {})
      options[:timeout] = timeout
      Proxy.new(options)
    end

    def with_wait(options)
      original_timeout = RSpec.configuration.wait_timeout
      original_delay = RSpec.configuration.wait_delay

      RSpec.configuration.wait_timeout = options[:timeout] if options[:timeout]
      RSpec.configuration.wait_delay = options[:delay] if options[:delay]

      yield
    ensure
      RSpec.configuration.wait_timeout = original_timeout
      RSpec.configuration.wait_delay = original_delay
    end
  end
end

RSpec.configure do |config|
  config.include(RSpec::Wait)

  config.add_setting(:wait_timeout, default: 10)
  config.add_setting(:wait_delay, default: 0.1)

  config.around do |example|
    if options = example.metadata[:wait]
      with_wait(options) { example.run }
    else
      example.run
    end
  end
end
