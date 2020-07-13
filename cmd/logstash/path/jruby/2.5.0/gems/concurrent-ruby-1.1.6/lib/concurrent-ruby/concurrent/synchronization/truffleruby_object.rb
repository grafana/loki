module Concurrent
  module Synchronization

    # @!visibility private
    module TruffleRubyAttrVolatile
      def self.included(base)
        base.extend(ClassMethods)
      end

      module ClassMethods
        def attr_volatile(*names)
          names.each do |name|
            ivar = :"@volatile_#{name}"

            class_eval <<-RUBY, __FILE__, __LINE__ + 1
              def #{name}
                full_memory_barrier
                #{ivar}                  
              end

              def #{name}=(value)
                #{ivar} = value
                full_memory_barrier
              end
            RUBY
          end

          names.map { |n| [n, :"#{n}="] }.flatten
        end
      end

      def full_memory_barrier
        TruffleRuby.full_memory_barrier
      end
    end

    # @!visibility private
    # @!macro internal_implementation_note
    class TruffleRubyObject < AbstractObject
      include TruffleRubyAttrVolatile

      def initialize
        # nothing to do
      end
    end
  end
end
