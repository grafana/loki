module Elasticsearch
  module API
    module Cat
      module Actions

        # Return currently running tasks
        #
        # @option arguments [String] :format a short version of the Accept header, e.g. json, yaml
        # @option arguments [List] :node_id A comma-separated list of node IDs or names to limit the returned information; use `_local` to return information from the node you're connecting to, leave empty to get information from all nodes
        # @option arguments [List] :actions A comma-separated list of actions that should be returned. Leave empty to return all.
        # @option arguments [Boolean] :detailed Return detailed task information (default: false)
        # @option arguments [String] :parent_node Return tasks with specified parent node.
        # @option arguments [Number] :parent_task Return tasks with specified parent task id. Set to -1 to return all.
        # @option arguments [List] :h Comma-separated list of column names to display
        # @option arguments [Boolean] :help Return help information
        # @option arguments [Boolean] :v Verbose mode. Display column headers
        # @option arguments [List] :s Comma-separated list of column names or column aliases to sort by
        #
        # @see http://www.elastic.co/guide/en/elasticsearch/reference/master/tasks.html
        #
        def tasks(arguments={})
          valid_params = [
            :format,
            :node_id,
            :actions,
            :detailed,
            :parent_node,
            :parent_task,
            :h,
            :help,
            :v,
            :s ]
          method = 'GET'
          path   = "_cat/tasks"
          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
