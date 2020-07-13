module Elasticsearch
  module API
    module Tasks
      module Actions

        # Return the list of tasks
        #
        # @option arguments [Number] :task_id Return the task with specified id
        # @option arguments [List] :node_id A comma-separated list of node IDs or names to limit the returned
        #                                   information; use `_local` to return information from the node
        #                                   you're connecting to, leave empty to get information from all nodes
        # @option arguments [List] :actions A comma-separated list of actions that should be returned.
        #                                   Leave empty to return all.
        # @option arguments [Boolean] :detailed Return detailed task information (default: false)
        # @option arguments [String] :parent_node Return tasks with specified parent node.
        # @option arguments [Number] :parent_task Return tasks with specified parent task id.
        #                                         Set to -1 to return all.
        # @option arguments [String] :group_by Group tasks by nodes or parent/child relationships
        #                                      Options: nodes, parents
        # @option arguments [Boolean] :wait_for_completion Wait for the matching tasks to complete (default: false)
        #
        # @see http://www.elastic.co/guide/en/elasticsearch/reference/master/tasks-list.html
        #
        def list(arguments={})
          valid_params = [
            :node_id,
            :actions,
            :detailed,
            :parent_node,
            :parent_task,
            :group_by,
            :wait_for_completion ]

          arguments = arguments.clone

          task_id = arguments.delete(:task_id)

          method = 'GET'
          path   = Utils.__pathify( '_tasks', Utils.__escape(task_id) )
          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
