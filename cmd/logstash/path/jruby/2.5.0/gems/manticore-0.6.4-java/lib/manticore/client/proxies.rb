module Manticore
  class Client
    module ProxiesInterface
      def respond_with(stubs)
        StubProxy.new(self, stubs)
      end

      # Causes the next request to be made asynchronously
      def async
        AsyncProxy.new(self)
      end

      alias_method :parallel, :async
      alias_method :batch, :async

      # Causes the next request to be made immediately in the background
      def background
        BackgroundProxy.new(self)
      end
    end

    class BaseProxy
      include ProxiesInterface

      def initialize(client)
        @client = client
      end
    end

    class AsyncProxy < BaseProxy
      %w(get put head post delete options patch).each do |func|
        define_method func do |url, options = {}, &block|
          @client.send(func, url, options.merge(async: true), &block)
        end
      end
    end

    class StubProxy < BaseProxy
      def initialize(client, stubs)
        super(client)
        @stubs = stubs
      end

      %w(get put head post delete options patch).each do |func|
        define_method func do |url, options = {}, &block|
          @client.stub(url, @stubs)
          @client.send(func, url, options, &block).complete { @client.unstub url }
        end
      end
    end

    class BackgroundProxy < BaseProxy
      %w(get put head post delete options patch).each do |func|
        define_method func do |url, options = {}, &block|
          @client.send(func, url, options.merge(async_background: true), &block)
        end
      end
    end
  end
end
