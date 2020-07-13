require 'gems/client'
require 'gems/configuration'

module Gems
  extend Configuration
  class << self
    # Alias for Gems::Client.new
    #
    # @return [Gems::Client]
    def new(options = {})
      Gems::Client.new(options)
    end

    # Delegate to Gems::Client
    def method_missing(method, *args, &block)
      return super unless new.respond_to?(method)
      new.send(method, *args, &block)
    end

    def respond_to?(method_name, include_private = false)
      new.respond_to?(method_name, include_private) || super(method_name, include_private)
    end

    def respond_to_missing?(method_name, include_private = false)
      new.respond_to?(method_name, include_private) || super(method_name, include_private)
    end
  end
end
