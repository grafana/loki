module Elasticsearch
  module API
    module Tasks
      module Actions

        # Return information about a specific task
        #
        # @option arguments [String] :task_id Return the task with specified id (node_id:task_number)
        # @option arguments [Boolean] :wait_for_completion Wait for the matching tasks to complete (default: false)
        #
        # @see http://www.elastic.co/guide/en/elasticsearch/reference/master/tasks.html
        #
        def get(arguments={})
          valid_params = [ :wait_for_completion ]

          arguments = arguments.clone

          task_id = arguments.delete(:task_id)

          method = HTTP_GET
          path   = Utils.__pathify '_tasks', Utils.__escape(task_id)
          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
