require 'concurrent/utility/engine'
require 'concurrent/thread_safe/util'

module Concurrent

  # @!macro concurrent_array
  #
  #   A thread-safe subclass of Array. This version locks against the object
  #   itself for every method call, ensuring only one thread can be reading
  #   or writing at a time. This includes iteration methods like `#each`.
  #
  #   @note `a += b` is **not** a **thread-safe** operation on
  #     `Concurrent::Array`. It reads array `a`, then it creates new `Concurrent::Array`
  #     which is concatenation of `a` and `b`, then it writes the concatenation to `a`.
  #     The read and write are independent operations they do not form a single atomic
  #     operation therefore when two `+=` operations are executed concurrently updates
  #     may be lost. Use `#concat` instead.
  #
  #   @see http://ruby-doc.org/core-2.2.0/Array.html Ruby standard library `Array`

  # @!macro internal_implementation_note
  ArrayImplementation = case
                        when Concurrent.on_cruby?
                          # Array is thread-safe in practice because CRuby runs
                          # threads one at a time and does not do context
                          # switching during the execution of C functions.
                          ::Array

                        when Concurrent.on_jruby?
                          require 'jruby/synchronized'

                          class JRubyArray < ::Array
                            include JRuby::Synchronized
                          end
                          JRubyArray

                        when Concurrent.on_rbx?
                          require 'monitor'
                          require 'concurrent/thread_safe/util/data_structures'

                          class RbxArray < ::Array
                          end

                          ThreadSafe::Util.make_synchronized_on_rbx RbxArray
                          RbxArray

                        when Concurrent.on_truffleruby?
                          require 'concurrent/thread_safe/util/data_structures'

                          class TruffleRubyArray < ::Array
                          end

                          ThreadSafe::Util.make_synchronized_on_truffleruby TruffleRubyArray
                          TruffleRubyArray

                        else
                          warn 'Possibly unsupported Ruby implementation'
                          ::Array
                        end
  private_constant :ArrayImplementation

  # @!macro concurrent_array
  class Array < ArrayImplementation
  end

end
