require 'concurrent/maybe'

module Concurrent
  class Channel
    class Selector

      class TakeClause

        def initialize(channel, block)
          @channel = channel
          @block = block
        end

        def execute
          if (result = @channel.poll?).just?
            Concurrent::Maybe.just(@block.call(result.value))
          else
            Concurrent::Maybe.nothing
          end
        end
      end
    end
  end
end
