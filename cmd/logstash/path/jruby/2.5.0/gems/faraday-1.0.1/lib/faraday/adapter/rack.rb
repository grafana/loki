# frozen_string_literal: true

module Faraday
  class Adapter
    # Sends requests to a Rack app.
    #
    # @example
    #
    #   class MyRackApp
    #     def call(env)
    #       [200, {'Content-Type' => 'text/html'}, ["hello world"]]
    #     end
    #   end
    #
    #   Faraday.new do |conn|
    #     conn.adapter :rack, MyRackApp.new
    #   end
    class Rack < Faraday::Adapter
      dependency 'rack/test'

      # not prefixed with "HTTP_"
      SPECIAL_HEADERS = %w[CONTENT_LENGTH CONTENT_TYPE].freeze

      def initialize(faraday_app, rack_app)
        super(faraday_app)
        mock_session = ::Rack::MockSession.new(rack_app)
        @session     = ::Rack::Test::Session.new(mock_session)
      end

      def call(env)
        super
        rack_env = build_rack_env(env)

        env[:request_headers]&.each do |name, value|
          name = name.upcase.tr('-', '_')
          name = "HTTP_#{name}" unless SPECIAL_HEADERS.include? name
          rack_env[name] = value
        end

        timeout = request_timeout(:open, env[:request])
        timeout ||= request_timeout(:read, env[:request])
        response = if timeout
                     Timer.timeout(timeout, Faraday::TimeoutError) do
                       execute_request(env, rack_env)
                     end
                   else
                     execute_request(env, rack_env)
                   end

        if (req = env[:request]).stream_response?
          warn "Streaming downloads for #{self.class.name} " \
            'are not yet implemented.'
          req.on_data.call(response.body, response.body.bytesize)
        end

        save_response(env, response.status, response.body, response.headers)
        @app.call env
      end

      private

      def execute_request(env, rack_env)
        @session.request(env[:url].to_s, rack_env)
      end

      def build_rack_env(env)
        {
          method: env[:method],
          input: env[:body].respond_to?(:read) ? env[:body].read : env[:body],
          'rack.url_scheme' => env[:url].scheme
        }
      end
    end
  end
end
