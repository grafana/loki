require 'concurrent/synchronization'
require 'concurrent/utility/monotonic_time'

module Concurrent
  class Channel

    # A convenience class representing a single moment in monotonic time.
    # Returned by {Concurrent::Channel} tickers and timers when they
    # resolve.
    #
    # Includes `Comparable` and can be compared to monotonic_time, UTC
    # time, or epoch time.
    #
    # @see Concurrent.monotonic_time
    # @see Concurrent::Channel.ticker
    # @see Concurrent::Channel.timer
    class Tick < Synchronization::Object
      include Comparable
      safe_initialization!

      STRING_FORMAT = '%F %T.%6N %z %Z'.freeze

      attr_reader :monotonic, :utc

      def initialize(tick = Concurrent.monotonic_time)
        @monotonic = tick
        @utc = monotonic_to_utc(tick).freeze
      end

      def epoch
        @utc.to_f
      end

      def to_s
        @utc.strftime(STRING_FORMAT)
      end

      def <=>(other)
        if other.is_a? Numeric
          @monotonic <=> other
        elsif other.is_a? Time
          @utc <=> other.utc
        elsif other.is_a? Tick
          @monotonic <=> other.monotonic
        else
          nil
        end
      end

      private

      def monotonic_to_utc(tick)
        Time.now.utc + Concurrent.monotonic_time - tick
      end
    end
  end
end
