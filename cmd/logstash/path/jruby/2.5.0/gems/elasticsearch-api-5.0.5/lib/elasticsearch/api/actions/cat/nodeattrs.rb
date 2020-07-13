module Elasticsearch
  module API
    module Cat
      module Actions

        # Display custom node attributes
        #
        # @option arguments [Boolean] :local Return local information, do not retrieve the state from master node (default: false)
        # @option arguments [Time] :master_timeout Explicit operation timeout for connection to master node
        # @option arguments [List] :h Comma-separated list of column names to display
        # @option arguments [Boolean] :help Return help information
        # @option arguments [Boolean] :v Verbose mode. Display column headers
        # @option arguments [List] :s Comma-separated list of column names or column aliases to sort by
        #
        # @see http://www.elastic.co/guide/en/elasticsearch/reference/master/cat-nodeattrs.html
        #
        def nodeattrs(arguments={})
          valid_params = [
            :local,
            :master_timeout,
            :h,
            :help,
            :v,
            :s ]
          method = 'GET'
          path   = "_cat/nodeattrs"
          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
