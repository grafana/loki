require 'concurrent/constants'
require 'concurrent/utility/monotonic_time'
require 'concurrent/channel/tick'
require 'concurrent/channel/buffer/base'

module Concurrent
  class Channel
    module Buffer

      class Timer < Base

        def put(item)
          false
        end

        def offer(item)
          false
        end

        def take
          loop do
            tick, _ = do_poll
            if tick
              return tick
            else
              Thread.pass
            end
          end
        end

        def next
          loop do
            tick, more = do_poll
            return tick, more if tick
            Thread.pass
          end
        end

        def poll
          tick, _ = do_poll
          tick = Concurrent::NULL unless tick
          tick
        end

        private

        def ns_initialize(delay)
          @tick = Concurrent.monotonic_time + delay.to_f
          self.capacity = 1
        end

        def ns_size
          0
        end

        def ns_empty?
          false
        end

        def ns_full?
          true
        end

        def do_poll
          synchronize do
            if ns_closed?
              return Concurrent::NULL, false
            elsif Concurrent.monotonic_time >= @tick
              # only one listener gets notified
              self.closed = true
              return Concurrent::Channel::Tick.new(@tick), false
            else
              return nil, true
            end
          end
        end
      end
    end
  end
end
