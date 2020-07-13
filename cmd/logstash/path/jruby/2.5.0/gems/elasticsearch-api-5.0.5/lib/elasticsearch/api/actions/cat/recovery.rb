module Elasticsearch
  module API
    module Cat
      module Actions

        # Display information about the recovery process (allocating shards)
        #
        # @example Display information for all indices
        #
        #     puts client.cat.recovery
        #
        # @example Display information for a specific index
        #
        #     puts client.cat.recovery index: 'index-a'
        #
        # @example Display information for indices matching a pattern
        #
        #     puts client.cat.recovery index: 'index-*'
        #
        # @example Display header names in the output
        #
        #     puts client.cat.recovery v: true
        #
        # @example Display only specific columns in the output (see the `help` parameter)
        #
        #     puts client.cat.recovery h: ['node', 'index', 'shard', 'percent']
        #
        # @example Display only specific columns in the output, using the short names
        #
        #     puts client.cat.recovery h: 'n,i,s,per'
        #
        # @example Return the information as Ruby objects
        #
        #     client.cat.recovery format: 'json'
        #
        # @option arguments [List] :index A comma-separated list of index names to limit the returned information
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
        # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/master/cat-recovery.html
        #
        def recovery(arguments={})
          valid_params = [
            :bytes,
            :local,
            :master_timeout,
            :h,
            :help,
            :v,
            :s ]

          index = arguments.delete(:index)

          method = HTTP_GET

          path   = Utils.__pathify '_cat/recovery', Utils.__listify(index)

          params = Utils.__validate_and_extract_params arguments, valid_params
          params[:h] = Utils.__listify(params[:h]) if params[:h]

          body   = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
