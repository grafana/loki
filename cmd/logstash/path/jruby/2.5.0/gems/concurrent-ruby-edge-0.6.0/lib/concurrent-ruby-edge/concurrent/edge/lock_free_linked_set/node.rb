require 'concurrent/atomic/atomic_markable_reference'

module Concurrent
  module Edge
    class LockFreeLinkedSet
      class Node < Synchronization::Object
        include Comparable

        safe_initialization!

        def initialize(data = nil, successor = nil)
          super()
          @SuccessorReference  = AtomicMarkableReference.new(successor || Tail.new)
          @Data                = data
          @Key                 = key_for data
        end

        def data
          @Data
        end

        def successor_reference
          @SuccessorReference
        end

        def key
          @Key
        end

        # Check to see if the node is the last in the list.
        def last?
          @SuccessorReference.value.is_a? Tail
        end

        # Next node in the list. Note: this is not the AtomicMarkableReference
        # of the next node, this is the actual Node itself.
        def next_node
          @SuccessorReference.value
        end

        # This method provides a unqiue key for the data which will be used for
        # ordering. This is configurable, and changes depending on how you wish
        # the nodes to be ordered.
        def key_for(data)
          data.hash
        end

        # We use `Object#hash` as a way to enforce ordering on the nodes. This
        # can be configurable in the future; for example, you could enforce a
        # split-ordering on the nodes in the set.
        def <=>(other)
          @Key <=> other.hash
        end
      end

      # Internal sentinel node for the Tail. It is always greater than all
      # other nodes, and  it is self-referential; meaning its successor is
      # a self-loop.
      class Tail < Node
        def initialize(_data = nil, _succ = nil)
          @SuccessorReference = AtomicMarkableReference.new self
        end

        # Always greater than other nodes. This means that traversal will end
        # at the tail node since we are comparing node size in the traversal.
        def <=>(_other)
          1
        end
      end


      # Internal sentinel node for the Head of the set. Head is always smaller
      # than any other node.
      class Head < Node
        def <=>(_other)
          -1
        end
      end
    end
  end
end
