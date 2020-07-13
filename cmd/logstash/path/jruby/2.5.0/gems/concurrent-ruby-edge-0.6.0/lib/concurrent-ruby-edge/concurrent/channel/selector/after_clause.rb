require 'concurrent/maybe'
require 'concurrent/utility/monotonic_time'

module Concurrent
  class Channel
    class Selector

      class AfterClause

        def initialize(seconds, block)
          raise ArgumentError.new('timeout must 0.0 or more') if seconds.to_f < 0.0
          @end = Concurrent.monotonic_time + seconds.to_f
          @block = block
        end

        def execute
          if Concurrent.monotonic_time > @end
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
