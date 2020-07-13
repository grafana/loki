require 'concurrent/thread_safe/util'

# Shim for TruffleRuby.synchronized
if Concurrent.on_truffleruby? && !TruffleRuby.respond_to?(:synchronized)
  module TruffleRuby
    def self.synchronized(object, &block)
      Truffle::System.synchronized(object, &block)
    end
  end
end

module Concurrent
  module ThreadSafe
    module Util
      def self.make_synchronized_on_rbx(klass)
        klass.class_eval do
          private

          def _mon_initialize
            @_monitor = Monitor.new unless @_monitor # avoid double initialisation
          end

          def self.new(*args)
            obj = super(*args)
            obj.send(:_mon_initialize)
            obj
          end
        end

        klass.superclass.instance_methods(false).each do |method|
          case method
          when :new_range, :new_reserved
            klass.class_eval <<-RUBY, __FILE__, __LINE__ + 1
              def #{method}(*args)
                obj = super
                obj.send(:_mon_initialize)
                obj
              end
            RUBY
          else
            klass.class_eval <<-RUBY, __FILE__, __LINE__ + 1
              def #{method}(*args)
                monitor = @_monitor
                monitor or raise("BUG: Internal monitor was not properly initialized. Please report this to the concurrent-ruby developers.")
                monitor.synchronize { super }
              end
            RUBY
          end
        end
      end

      def self.make_synchronized_on_truffleruby(klass)
        klass.superclass.instance_methods(false).each do |method|
          klass.class_eval <<-RUBY, __FILE__, __LINE__ + 1
            def #{method}(*args, &block)    
              TruffleRuby.synchronized(self) { super(*args, &block) }
            end
          RUBY
        end
      end
    end
  end
end
