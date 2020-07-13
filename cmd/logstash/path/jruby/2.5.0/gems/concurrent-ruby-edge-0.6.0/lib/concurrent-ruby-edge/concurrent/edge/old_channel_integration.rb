module Concurrent
  module Promises
    module FactoryMethods

      # @!visibility private

      module OldChannelIntegration

        # @!visibility private

        # only proof of concept
        # @return [Future]
        def select(*channels)
          # TODO (pitr-ch 26-Mar-2016): re-do, has to be non-blocking
          future do
            # noinspection RubyArgCount
            Channel.select do |s|
              channels.each do |ch|
                s.take(ch) { |value| [value, ch] }
              end
            end
          end
        end
      end

      include OldChannelIntegration
    end

    class Future < AbstractEventFuture

      # @!visibility private

      module OldChannelIntegration

        # @!visibility private

        # Zips with selected value form the suplied channels
        # @return [Future]
        def then_select(*channels)
          future = Concurrent::Promises.select(*channels)
          ZipFuturesPromise.new_blocked_by2(self, future, @DefaultExecutor).future
        end

        # @note may block
        # @note only proof of concept
        def then_put(channel)
          on_fulfillment_using(:io, channel) { |value, channel| channel.put value }
        end
      end

      include OldChannelIntegration
    end
  end
end
