module Elasticsearch
  module API
    module Cat
      module Actions

        # Display a terse version of the {Elasticsearch::API::Cluster::Actions#health} API output
        #
        # @example Display cluster health
        #
        #     puts client.cat.health
        #
        # @example Display header names in the output
        #
        #     puts client.cat.health v: true
        #
        # @example Return the information as Ruby objects
        #
        #     client.cat.health format: 'json'
        #
        # @option arguments [Boolean] :ts Whether to display timestamp information
        # @option arguments [List] :h Comma-separated list of column names to display -- see the `help` argument
        # @option arguments [Boolean] :v Display column headers as part of the output
        # @option arguments [List] :s Comma-separated list of column names or column aliases to sort by
        # @option arguments [String] :format The output format. Options: 'text', 'json'; default: 'text'
        # @option arguments [Boolean] :help Return information about headers
        # @option arguments [Boolean] :local Return local information, do not retrieve the state from master node
        #                                    (default: false)
        # @option arguments [Time] :master_timeout Explicit operation timeout for connection to master node
        #
        # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/master/cat-health.html
        #
        def health(arguments={})
          valid_params = [
            :local,
            :master_timeout,
            :h,
            :help,
            :ts,
            :v,
            :s ]

          method = HTTP_GET
          path   = "_cat/health"
          params = Utils.__validate_and_extract_params arguments, valid_params
          params[:h] = Utils.__listify(params[:h]) if params[:h]
          body   = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
