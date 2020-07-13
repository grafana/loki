require 'concurrent/edge/lock_free_linked_set/node'
require 'concurrent/edge/lock_free_linked_set/window'

module Concurrent
  module Edge
    # This class implements a lock-free linked set. The general idea of this
    # implementation is this: each node has a successor which is an Atomic
    # Markable Reference. This is used to ensure that all modifications to the
    # list are atomic, preserving the structure of the linked list under _any_
    # circumstance in a multithreaded application.
    #
    # One interesting aspect of this algorithm occurs with removing a node.
    # Instead of physically removing a node when remove is called, a node is
    # logically removed, by 'marking it.' By doing this, we prevent calls to
    # `remove` from traversing the list twice to perform a physical removal.
    # Instead, we have have calls to `add` and `remove` clean up all marked
    # nodes they encounter while traversing the list.
    #
    # This algorithm is a variation of the Nonblocking Linked Set found in
    # 'The Art of Multiprocessor Programming' by Herlihy and Shavit.
    # @!macro warn.edge
    class LockFreeLinkedSet
      include Enumerable

      # @!macro lock_free_linked_list_method_initialize
      #
      #   @param [Fixnum] initial_size the size of the linked_list to initialize
      def initialize(initial_size = 0, val = nil)
        @head = Head.new

        initial_size.times do
          val = block_given? ? yield : val
          add val
        end
      end

      # @!macro lock_free_linked_list_method_add
      #
      #   Atomically adds the item to the set if it does not yet exist. Note:
      #   internally the set uses `Object#hash` to compare equality of items,
      #   meaning that Strings and other objects will be considered equal
      #   despite being different objects.
      #
      #   @param [Object] item the item you wish to insert
      #
      #   @return [Boolean] `true` if successful. A `false` return indicates
      #   that the item was already in the set.
      def add(item)
        loop do
          window = Window.find @head, item

          pred, curr = window.pred, window.curr

          # Item already in set
          return false if curr == item

          node = Node.new item, curr

          if pred.successor_reference.compare_and_set curr, node, false, false
            return true
          end
        end
      end

      # @!macro lock_free_linked_list_method_<<
      #
      #   Atomically adds the item to the set if it does not yet exist.
      #
      #   @param [Object] item the item you wish to insert
      #
      #   @return [Object] the set on which the :<< method was invoked
      def <<(item)
        add item
        self
      end

      # @!macro lock_free_linked_list_method_contains
      #
      #   Atomically checks to see if the set contains an item. This method
      #   compares equality based on the `Object#hash` method, meaning that the
      #   hashed contents of an object is what determines equality instead of
      #   `Object#object_id`
      #
      #   @param [Object] item the item you to check for presence in the set
      #
      #   @return [Boolean] whether or not the item is in the set
      def contains?(item)
        curr = @head

        while curr < item
          curr = curr.next_node
          marked = curr.successor_reference.marked?
        end

        curr == item && !marked
      end

      # @!macro lock_free_linked_list_method_remove
      #
      #   Atomically attempts to remove an item, comparing using `Object#hash`.
      #
      #   @param [Object] item the item you to remove from the set
      #
      #   @return [Boolean] whether or not the item was removed from the set
      def remove(item)
        loop do
          window = Window.find @head, item
          pred, curr = window.pred, window.curr

          return false if curr != item

          succ = curr.next_node
          removed = curr.successor_reference.compare_and_set succ, succ, false, true

          #next_node unless removed
          next unless removed

          pred.successor_reference.compare_and_set curr, succ, false, false

          return true
        end
      end

      # @!macro lock_free_linked_list_method_each
      #
      #   An iterator to loop through the set.
      #
      #   @yield [item] each item in the set
      #   @yieldparam [Object] item the item you to remove from the set
      #
      #   @return [Object] self: the linked set on which each was called
      def each
        return to_enum unless block_given?

        curr = @head

        until curr.last?
          curr = curr.next_node
          marked = curr.successor_reference.marked?

          yield curr.data unless marked
        end

        self
      end
    end
  end
end
