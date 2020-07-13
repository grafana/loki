module Concurrent
  class Channel
    class Selector

      class ErrorClause

        def initialize(block)
          @block = block
        end

        def execute(error)
          @block.call(error)
        rescue
          # suppress and move on
        ensure
          return nil
        end
      end
    end
  end
end
