require "forwardable"

module Manticore
  # Mix-in that can be used to add Manticore functionality to arbitrary classes.
  # Its primary purpose is to extend the Manticore module for one-shot usage.
  #
  # @example  Simple mix-in usage
  #     class Foo
  #       include Manticore::Facade
  #       include_http_client pool_size: 5
  #
  #       def latest_results
  #         Foo.get "http://some.com/url"
  #       end
  #     end
  module Facade
    # @private
    def self.included(other)
      other.send :extend, ClassMethods
    end

    module ClassMethods
      # Adds basic synchronous Manticore::Client functionality to the receiver.
      # @param  options Hash Options to be passed to the underlying shared client, if it has not been created yet.
      # @return nil
      def include_http_client(options = {}, &block)
        @__manticore_facade_options = [options, block]
        class << self
          extend Forwardable
          def_delegators "__manticore_facade", :get, :head, :put, :post, :delete, :options, :patch, :http
        end
        nil
      end

      private

      def __manticore_facade
        @manticore_facade ||= begin
          options, block = *@__manticore_facade_options
          if shared_pool = options.delete(:shared_pool)
            Manticore.send(:__manticore_facade)
          else
            Manticore::Client.new(options, &block)
          end
        end
      end
    end
  end
end
