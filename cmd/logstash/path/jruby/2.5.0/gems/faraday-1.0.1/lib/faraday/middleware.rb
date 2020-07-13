# frozen_string_literal: true

module Faraday
  # Middleware is the basic base class of any Faraday middleware.
  class Middleware
    extend MiddlewareRegistry
    extend DependencyLoader

    def initialize(app = nil)
      @app = app
    end

    def close
      if @app.respond_to?(:close)
        @app.close
      else
        warn "#{@app} does not implement \#close!"
      end
    end
  end
end
