require 'concurrent/maybe'

module Concurrent
  class Channel
    class Selector

      class PutClause

        def initialize(channel, message, block)
          @channel = channel
          @message = message
          @block = block
        end

        def execute
          if @channel.offer?(@message).just?
            result = @block ? @block.call : nil
            Concurrent::Maybe.just(result)
          else
            Concurrent::Maybe.nothing
          end
        end
      end
    end
  end
end
