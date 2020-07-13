module Elasticsearch
  module API
    module Cat
      module Actions

        # Display information about cluster topology and nodes statistics
        #
        # @example Display basic information about nodes in the cluster (host, node name, memory usage, master, etc.)
        #
        #     puts client.cat.nodes
        #
        # @example Display header names in the output
        #
        #     puts client.cat.nodes v: true
        #
        # @example Display only specific columns in the output (see the `help` parameter)
        #
        #     puts client.cat.nodes h: %w(name version jdk disk.avail heap.percent load merges.total_time), v: true
        #
        # @example Display only specific columns in the output, using the short names
        #
        #     puts client.cat.nodes h: 'n,v,j,d,hp,l,mtt', v: true
        #
        # @example Return the information as Ruby objects
        #
        #     client.cat.nodes format: 'json'
        #
        # @option arguments [Boolean] :full_id Return the full node ID instead of the shortened version (default: false)
        # @option arguments [List] :h Comma-separated list of column names to display -- see the `help` argument
        # @option arguments [Boolean] :v Display column headers as part of the output
        # @option arguments [List] :s Comma-separated list of column names or column aliases to sort by
        # @option arguments [String] :format The output format. Options: 'text', 'json'; default: 'text'
        # @option arguments [Boolean] :help Return information about headers
        # @option arguments [Boolean] :local Return local information, do not retrieve the state from master node
        #                                    (default: false)
        # @option arguments [Time] :master_timeout Explicit operation timeout for connection to master node
        #
        # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/master/cat-nodes.html
        #
        def nodes(arguments={})
          valid_params = [
            :full_id,
            :local,
            :master_timeout,
            :h,
            :help,
            :v,
            :s ]

          method = HTTP_GET
          path   = "_cat/nodes"

          params = Utils.__validate_and_extract_params arguments, valid_params
          params[:h] = Utils.__listify(params[:h], :escape => false) if params[:h]

          body   = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
