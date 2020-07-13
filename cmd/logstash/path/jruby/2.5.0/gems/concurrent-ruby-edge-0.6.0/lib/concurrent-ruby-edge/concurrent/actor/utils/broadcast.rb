require 'set'

module Concurrent
  module Actor
    module Utils

      # Allows to build pub/sub easily.
      # @example news
      #   news_channel = Concurrent::Actor::Utils::Broadcast.spawn :news
      #
      #   2.times do |i|
      #     Concurrent::Actor::Utils::AdHoc.spawn "listener-#{i}" do
      #       news_channel << :subscribe
      #       -> message { puts message }
      #     end
      #   end
      #
      #   news_channel << 'Ruby rocks!'
      #   # prints: 'Ruby rocks!' twice
      class Broadcast < RestartingContext

        def initialize
          @receivers = Set.new
        end

        def on_message(message)
          case message
          when :subscribe
            if envelope.sender.is_a? Reference
              @receivers.add envelope.sender
              true
            else
              false
            end
          when :unsubscribe
            !!@receivers.delete(envelope.sender)
          when :subscribed?
            @receivers.include? envelope.sender
          else
            filtered_receivers.each { |r| r << message }
          end
        end

        # override to define different behaviour, filtering etc
        def filtered_receivers
          @receivers
        end
      end

    end
  end
end
