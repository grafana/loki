module Elasticsearch
  module API
    module Cat
      module Actions

        # Display basic information about the master node
        #
        # @example
        #
        #     puts client.cat.master
        #
        # @example Display header names in the output
        #
        #     puts client.cat.master v: true
        #
        # @example Return the information as Ruby objects
        #
        #     client.cat.master format: 'json'
        #
        # @option arguments [List] :h Comma-separated list of column names to display -- see the `help` argument
        # @option arguments [Boolean] :v Display column headers as part of the output
        # @option arguments [List] :s Comma-separated list of column names or column aliases to sort by
        # @option arguments [String] :format The output format. Options: 'text', 'json'; default: 'text'
        # @option arguments [Boolean] :help Return information about headers
        # @option arguments [Boolean] :local Return local information, do not retrieve the state from master node
        #                                    (default: false)
        # @option arguments [Time] :master_timeout Explicit operation timeout for connection to master node
        #
        # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/master/cat-master.html
        #
        def master(arguments={})
          valid_params = [
            :local,
            :master_timeout,
            :h,
            :help,
            :v,
            :s ]

          method = HTTP_GET
          path   = "_cat/master"
          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
