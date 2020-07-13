require "faraday"

module Faraday
  class Adapter
    class Manticore < Faraday::Adapter
      dependency { require "manticore" }

      class ParallelManager
        def client=(client)
          @client ||= client
        end

        def run
          @client.execute! if @client
        end
      end

      self.supports_parallel = true
      def self.setup_parallel_manager(options = {})
        ParallelManager.new
      end

      def initialize(app, connection_options = {})
        @connection_options = connection_options
        super(app)
      end

      def client(env)
        @client ||= begin
          opts = {}
          if ssl = env[:ssl].to_hash
            opts[:ssl] = {}
            opts[:ssl][:verify] = :disable if ssl[:verify] == false
            opts[:ssl][:ca_file] = ssl[:ca_file]
            opts[:ssl][:client_cert] = ssl[:client_cert]
            opts[:ssl][:client_key] = ssl[:client_key]
          end
          conn_opts = @connection_options.dup
          if conn_opts.key?(:ssl)
            (opts[:ssl] ||= {}).merge! conn_opts.delete(:ssl)
          end
          opts.merge! conn_opts
          ::Manticore::Client.new(opts)
        end
      end

      def call(env)
        super

        opts = {}
        if env.key? :request_headers
          opts[:headers] = env[:request_headers]
          opts[:headers].reject! { |k, _| k.downcase == "content-length" }  # Manticore computes Content-Length
        end
        body = read_body(env)
        opts[:body] = body if body

        if req = env[:request]
          opts[:request_timeout] = opts[:socket_timeout] = opts[:connect_timeout] = req[:timeout] if req.key?(:timeout)
          opts[:connect_timeout] = req[:open_timeout] if req.key?(:open_timeout)
          if prx = req[:proxy]
            opts[:proxy] = {
              :url => prx[:uri].to_s,
              :user => prx[:user],
              :password => prx[:password],
            }
          end
        end

        cl = client(env)
        if parallel?(env)
          env[:parallel_manager].client = cl
          cl = cl.async
        end

        last_exception = nil

        req = cl.send(env[:method].to_s.downcase, env[:url].to_s, opts)
        req.on_success do |response|
          save_response(env, response.code, response.body || "", response.headers)
          env[:response].finish(env) if parallel?(env)
        end

        req.on_failure do |err|
          case err
          when ::Manticore::Timeout
            raise TimeoutError, err
          when ::Manticore::SocketException, ::Java::JavaUtilConcurrent::ExecutionException
            raise ConnectionFailed, err
          when ::Manticore::ClientProtocolException
            raise Faraday::ClientError, err
          else
            raise err
          end
        end

        req.call unless parallel?(env)
        @app.call env
      end

      def parallel?(env)
        !env[:parallel_manager].nil?
      end

      def read_body(env)
        env[:body].respond_to?(:read) ? env[:body].read : env[:body]
      end
    end

    register_middleware nil, :manticore => :Manticore
  end
end
