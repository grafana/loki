module Elasticsearch
  module API
    module Tasks
      module Actions

        # Cancel a specific task
        #
        # @option arguments [Number] :task_id Cancel the task with specified id
        # @option arguments [List] :node_id A comma-separated list of node IDs or names to limit the returned
        #                                   information; use `_local` to return information from the node
        #                                   you're connecting to, leave empty to get information from all nodes
        # @option arguments [List] :actions A comma-separated list of actions that should be returned.
        #                                   Leave empty to return all.
        # @option arguments [String] :parent_node Cancel tasks with specified parent node.
        # @option arguments [Number] :parent_task Cancel tasks with specified parent task id.
        #                                         Set to -1 to cancel all.
        #
        # @see http://www.elastic.co/guide/en/elasticsearch/reference/master/tasks-cancel.html
        #
        def cancel(arguments={})
          valid_params = [
            :node_id,
            :actions,
            :parent_node,
            :parent_task ]

          arguments = arguments.clone

          task_id = arguments.delete(:task_id)

          method = 'POST'
          path   = Utils.__pathify( '_tasks', Utils.__escape(task_id), '_cancel' )
          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
