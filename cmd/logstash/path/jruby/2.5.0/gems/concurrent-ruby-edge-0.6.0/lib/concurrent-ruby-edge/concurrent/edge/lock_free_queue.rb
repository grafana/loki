module Concurrent

  # @!visibility private
  class LockFreeQueue < Synchronization::Object

    class Node < Synchronization::Object
      attr_atomic :successor

      def initialize(item, successor)
        super()
        # published through queue, no need to be volatile or final
        @Item          = item
        self.successor = successor
      end

      def item
        @Item
      end
    end

    safe_initialization!

    attr_atomic :head, :tail

    def initialize
      super()
      dummy_node = Node.new(:dummy, nil)

      self.head = dummy_node
      self.tail = dummy_node
    end

    def push(item)
      # allocate a new node with the item embedded
      new_node = Node.new(item, nil)

      # keep trying until the operation succeeds
      while true
        current_tail_node      = tail
        current_tail_successor = current_tail_node.successor

        # if our stored tail is still the current tail
        if current_tail_node == tail
          # if that tail was really the last node
          if current_tail_successor.nil?
            # if we can update the previous successor of tail to point to this new node
            if current_tail_node.compare_and_set_successor(nil, new_node)
              # then update tail to point to this node as well
              compare_and_set_tail(current_tail_node, new_node)
              # and return
              return true
              # else, start the loop over
            end
          else
            # in this case, the tail ref we had wasn't the real tail
            # so we try to set its successor as the real tail, then start the loop again
            compare_and_set_tail(current_tail_node, current_tail_successor)
          end
        end
      end
    end

    def pop
      # retry until some value can be returned
      while true
        # the value in @head is just a dummy node that always sits in that position,
        # the real 'head' is in its successor
        current_dummy_node = head
        current_tail_node  = tail

        current_head_node = current_dummy_node.successor

        # if our local head is still consistent with the head node, continue
        # otherwise, start over
        if current_dummy_node == head
          # if either the queue is empty, or falling behind
          if current_dummy_node == current_tail_node
            # if there's nothing after the 'dummy' head node
            if current_head_node.nil?
              # just return nil
              return nil
            else
              # here the head element succeeding head is not nil, but the head and tail are equal
              # so tail is falling behind, update it, then start over
              compare_and_set_tail(current_tail_node, current_head_node)
            end

            # the queue isn't empty
            # if we can set the dummy head to the 'real' head, we're free to return the value in that real head, success
          elsif compare_and_set_head(current_dummy_node, current_head_node)
            # grab the item from the popped node
            item = current_head_node.item

            # return it, success!
            return item
          end
        end
      end
    end

    # approximate
    def size
      successor = head.successor
      count     = 0

      while true
        break if successor.nil?

        current_node = successor
        successor    = current_node.successor
        count        += 1
      end

      count
    end
  end
end
