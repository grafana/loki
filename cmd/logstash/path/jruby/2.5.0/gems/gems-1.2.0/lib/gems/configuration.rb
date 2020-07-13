require 'gems/version'
require 'rubygems'
require 'yaml'

module Gems
  module Configuration
    # An array of valid keys in the options hash when configuring a {Gems::Client}
    VALID_OPTIONS_KEYS = %i[
      host
      key
      password
      user_agent
      username
    ].freeze

    # Set the default API endpoint
    DEFAULT_HOST = ENV['RUBYGEMS_HOST'] ? ENV['RUBYGEMS_HOST'] : 'https://rubygems.org'

    # Set the default credentials
    DEFAULT_KEY = Gem.configuration.rubygems_api_key

    # Set the default 'User-Agent' HTTP header
    DEFAULT_USER_AGENT = "Gems #{Gems::VERSION}".freeze

    attr_accessor(*VALID_OPTIONS_KEYS)

    # When this module is extended, set all configuration options to their default values
    def self.extended(base)
      base.reset
    end

    # Convenience method to allow configuration options to be set in a block
    def configure
      yield self
    end

    # Create a hash of options and their values
    def options
      options = {}
      VALID_OPTIONS_KEYS.each { |k| options[k] = send(k) }
      options
    end

    # Reset all configuration options to defaults
    def reset
      self.username = nil
      self.password = nil
      self.host       = DEFAULT_HOST
      self.key        = DEFAULT_KEY
      self.user_agent = DEFAULT_USER_AGENT
      self
    end
  end
end
