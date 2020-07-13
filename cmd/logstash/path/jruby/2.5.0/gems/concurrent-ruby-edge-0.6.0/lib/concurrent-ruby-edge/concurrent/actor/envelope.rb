module Concurrent
  module Actor
    class Envelope
      include TypeCheck

      # @!attribute [r] message
      #   @return [Object] a message
      # @!attribute [r] future
      #   @return [Edge::Future] a future which becomes resolved after message is processed
      # @!attribute [r] sender
      #   @return [Reference, Thread] an actor or thread sending the message
      # @!attribute [r] address
      #   @return [Reference] where this message will be delivered

      attr_reader :message, :future, :sender, :address

      def initialize(message, future, sender, address)
        @message = message
        @future  = Type! future, Promises::ResolvableFuture, NilClass
        @sender  = Type! sender, Reference, Thread
        @address = Type! address, Reference
      end

      def sender_path
        if sender.is_a? Reference
          sender.path
        else
          sender.to_s
        end
      end

      def address_path
        address.path
      end

      def reject!(error)
        future.reject error unless future.nil?
      end
    end
  end
end
