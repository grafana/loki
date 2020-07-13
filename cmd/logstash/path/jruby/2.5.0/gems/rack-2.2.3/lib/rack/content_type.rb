# frozen_string_literal: true

module Rack

  # Sets the Content-Type header on responses which don't have one.
  #
  # Builder Usage:
  #   use Rack::ContentType, "text/plain"
  #
  # When no content type argument is provided, "text/html" is the
  # default.
  class ContentType
    include Rack::Utils

    def initialize(app, content_type = "text/html")
      @app, @content_type = app, content_type
    end

    def call(env)
      status, headers, body = @app.call(env)
      headers = Utils::HeaderHash[headers]

      unless STATUS_WITH_NO_ENTITY_BODY.key?(status.to_i)
        headers[CONTENT_TYPE] ||= @content_type
      end

      [status, headers, body]
    end
  end
end
