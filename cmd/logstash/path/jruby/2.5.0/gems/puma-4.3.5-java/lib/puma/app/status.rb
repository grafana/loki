# frozen_string_literal: true

require 'json'

module Puma
  module App
    # Check out {#call}'s source code to see what actions this web application
    # can respond to.
    class Status
      OK_STATUS = '{ "status": "ok" }'.freeze

      def initialize(cli, token = nil)
        @cli = cli
        @auth_token = token
      end

      def call(env)
        unless authenticate(env)
          return rack_response(403, 'Invalid auth token', 'text/plain')
        end

        case env['PATH_INFO']
        when /\/stop$/
          @cli.stop
          rack_response(200, OK_STATUS)

        when /\/halt$/
          @cli.halt
          rack_response(200, OK_STATUS)

        when /\/restart$/
          @cli.restart
          rack_response(200, OK_STATUS)

        when /\/phased-restart$/
          if !@cli.phased_restart
            rack_response(404, '{ "error": "phased restart not available" }')
          else
            rack_response(200, OK_STATUS)
          end

        when /\/reload-worker-directory$/
          if !@cli.send(:reload_worker_directory)
            rack_response(404, '{ "error": "reload_worker_directory not available" }')
          else
            rack_response(200, OK_STATUS)
          end

        when /\/gc$/
          GC.start
          rack_response(200, OK_STATUS)

        when /\/gc-stats$/
          rack_response(200, GC.stat.to_json)

        when /\/stats$/
          rack_response(200, @cli.stats)
        else
          rack_response 404, "Unsupported action", 'text/plain'
        end
      end

      private

      def authenticate(env)
        return true unless @auth_token
        env['QUERY_STRING'].to_s.split(/&;/).include?("token=#{@auth_token}")
      end

      def rack_response(status, body, content_type='application/json')
        headers = {
          'Content-Type' => content_type,
          'Content-Length' => body.bytesize.to_s
        }

        [status, headers, [body]]
      end
    end
  end
end
