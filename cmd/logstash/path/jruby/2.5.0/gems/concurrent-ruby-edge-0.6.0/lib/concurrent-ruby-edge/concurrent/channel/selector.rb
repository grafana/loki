require 'concurrent/maybe'

require 'concurrent/channel/selector/after_clause'
require 'concurrent/channel/selector/default_clause'
require 'concurrent/channel/selector/put_clause'
require 'concurrent/channel/selector/take_clause'

module Concurrent
  class Channel

    # @!visibility private
    class Selector

      def initialize
        @clauses = []
        @error_handler = nil
      end

      def case(channel, action, message = nil, &block)
        if [:take, :poll, :receive, :~].include?(action)
          take(channel, &block)
        elsif [:put, :offer, :send, :<<].include?(action)
          put(channel, message, &block)
        else
          raise ArgumentError.new('invalid action')
        end
      end

      def take(channel, &block)
        raise ArgumentError.new('no block given') unless block_given?
        @clauses << TakeClause.new(channel, block)
      end
      alias_method :receive, :take

      def put(channel, message, &block)
        @clauses << PutClause.new(channel, message, block)
      end
      alias_method :send, :put

      def after(seconds, &block)
        @clauses << AfterClause.new(seconds, block)
      end
      alias_method :timeout, :after

      def default(&block)
        raise ArgumentError.new('no block given') unless block_given?
        @clauses << DefaultClause.new(block)
      end

      def error(&block)
        raise ArgumentError.new('no block given') unless block_given?
        raise ArgumentError.new('only one error handler allowed') if @error_handler
        @error_handler = block
      end

      def execute
        raise Channel::Error.new('no clauses given') if @clauses.empty?
        loop do
          done = @clauses.each do |clause|
            result = clause.execute
            break result if result.just?
          end
          break done.value if done.is_a?(Concurrent::Maybe)
          Thread.pass
        end
      rescue => ex
        if @error_handler
          @error_handler.call(ex)
        else
          raise ex
        end
      end
    end
  end
end
