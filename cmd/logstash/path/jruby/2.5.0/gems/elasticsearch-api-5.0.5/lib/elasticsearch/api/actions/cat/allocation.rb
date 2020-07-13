module Elasticsearch
  module API
    module Cat
      module Actions

        # Return shard allocation information
        #
        # @example Display allocation for all nodes in the cluster
        #
        #     puts client.cat.allocation
        #
        # @example Display allocation for node with name 'node-1'
        #
        #     puts client.cat.allocation node_id: 'node-1'
        #
        # @example Display header names in the output
        #
        #     puts client.cat.allocation v: true
        #
        # @example Display only specific columns in the output (see the `help` parameter)
        #
        #     puts client.cat.allocation h: ['node', 'shards', 'disk.percent']
        #
        # @example Display only specific columns in the output, using the short names
        #
        #     puts client.cat.allocation h: 'n,s,dp'
        #
        # @example Return the information as Ruby objects
        #
        #     client.cat.allocation format: 'json'
        #
        # @option arguments [List] :node_id A comma-separated list of node IDs or names to limit the returned information
        # @option arguments [String] :bytes The unit in which to display byte values (options: b, k, m, g)
        # @option arguments [List] :h Comma-separated list of column names to display -- see the `help` argument
        # @option arguments [Boolean] :v Display column headers as part of the output
        # @option arguments [List] :s Comma-separated list of column names or column aliases to sort by
        # @option arguments [String] :format The output format. Options: 'text', 'json'; default: 'text'
        # @option arguments [Boolean] :help Return information about headers
        # @option arguments [Boolean] :local Return local information, do not retrieve the state from master node
        #                                    (default: false)
        # @option arguments [Time] :master_timeout Explicit operation timeout for connection to master node
        #
        # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/master/cat-allocation.html
        #
        def allocation(arguments={})
          valid_params = [
            :bytes,
            :local,
            :master_timeout,
            :h,
            :help,
            :v,
            :s ]

          node_id = arguments.delete(:node_id)

          method = HTTP_GET

          path   = Utils.__pathify '_cat/allocation', Utils.__listify(node_id)

          params = Utils.__validate_and_extract_params arguments, valid_params
          params[:h] = Utils.__listify(params[:h]) if params[:h]

          body   = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
