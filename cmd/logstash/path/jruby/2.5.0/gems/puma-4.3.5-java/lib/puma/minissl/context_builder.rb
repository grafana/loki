module Puma
  module MiniSSL
    class ContextBuilder
      def initialize(params, events)
        require 'puma/minissl'
        MiniSSL.check

        @params = params
        @events = events
      end

      def context
        ctx = MiniSSL::Context.new

        if defined?(JRUBY_VERSION)
          unless params['keystore']
            events.error "Please specify the Java keystore via 'keystore='"
          end

          ctx.keystore = params['keystore']

          unless params['keystore-pass']
            events.error "Please specify the Java keystore password  via 'keystore-pass='"
          end

          ctx.keystore_pass = params['keystore-pass']
          ctx.ssl_cipher_list = params['ssl_cipher_list'] if params['ssl_cipher_list']
        else
          unless params['key']
            events.error "Please specify the SSL key via 'key='"
          end

          ctx.key = params['key']

          unless params['cert']
            events.error "Please specify the SSL cert via 'cert='"
          end

          ctx.cert = params['cert']

          if ['peer', 'force_peer'].include?(params['verify_mode'])
            unless params['ca']
              events.error "Please specify the SSL ca via 'ca='"
            end
          end

          ctx.ca = params['ca'] if params['ca']
          ctx.ssl_cipher_filter = params['ssl_cipher_filter'] if params['ssl_cipher_filter']
        end

        ctx.no_tlsv1 = true if params['no_tlsv1'] == 'true'
        ctx.no_tlsv1_1 = true if params['no_tlsv1_1'] == 'true'

        if params['verify_mode']
          ctx.verify_mode = case params['verify_mode']
                            when "peer"
                              MiniSSL::VERIFY_PEER
                            when "force_peer"
                              MiniSSL::VERIFY_PEER | MiniSSL::VERIFY_FAIL_IF_NO_PEER_CERT
                            when "none"
                              MiniSSL::VERIFY_NONE
                            else
                              events.error "Please specify a valid verify_mode="
                              MiniSSL::VERIFY_NONE
                            end
        end

        ctx
      end

      private

      attr_reader :params, :events
    end
  end
end
