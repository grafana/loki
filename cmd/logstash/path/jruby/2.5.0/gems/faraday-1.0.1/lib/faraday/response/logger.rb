# frozen_string_literal: true

require 'forwardable'
require 'faraday/logging/formatter'

module Faraday
  class Response
    # Logger is a middleware that logs internal events in the HTTP request
    # lifecycle to a given Logger object. By default, this logs to STDOUT. See
    # Faraday::Logging::Formatter to see specifically what is logged.
    class Logger < Middleware
      def initialize(app, logger = nil, options = {})
        super(app)
        logger ||= begin
          require 'logger'
          ::Logger.new($stdout)
        end
        formatter_class = options.delete(:formatter) || Logging::Formatter
        @formatter = formatter_class.new(logger: logger, options: options)
        yield @formatter if block_given?
      end

      def call(env)
        @formatter.request(env)
        super
      end

      def on_complete(env)
        @formatter.response(env)
      end
    end
  end
end
