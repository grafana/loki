require "gem_publisher/cli_facade"

module GemPublisher
  class Pusher
    def initialize(cli_facade = CliFacade.new)
      @cli_facade = cli_facade
    end

    PUSH_METHODS = {
      "rubygems" => %w[gem push],
      "gemfury"  => %w[fury push]
    }

    # Publish the gem.
    #
    # Supported options:
    #   :as - specify a shared account to publish the gem (Gemfury only)
    def push(gem, method, options = {})
      push_command = PUSH_METHODS[method.to_s] or raise "Unknown Gem push method #{method.inspect}."
      push_command += [gem]
      push_command += ["--as", options[:as]] if options[:as]
      @cli_facade.execute *push_command
    end
  end
end

