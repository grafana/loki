require 'concurrent/constants'
require 'concurrent/utility/monotonic_time'
require 'concurrent/channel/tick'
require 'concurrent/channel/buffer/timer'

module Concurrent
  class Channel
    module Buffer

      class Ticker < Timer

        private

        def ns_initialize(interval)
          @interval = interval.to_f
          @next_tick = Concurrent.monotonic_time + interval
          self.capacity = 1
        end

        def do_poll
          synchronize do
            if ns_closed?
              return Concurrent::NULL, false
            elsif (now = Concurrent.monotonic_time) >= @next_tick
              tick = Concurrent::Channel::Tick.new(@next_tick)
              @next_tick = now + @interval
              return tick, true
            else
              return nil, true
            end
          end
        end
      end
    end
  end
end
