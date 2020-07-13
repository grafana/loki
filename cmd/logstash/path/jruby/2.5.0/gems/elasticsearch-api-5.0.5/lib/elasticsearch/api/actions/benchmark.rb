module Elasticsearch
  module API
    module Actions

      # Run a single query, or a set of queries, and return statistics on their performance
      #
      # @example Return statistics for a single query
      #
      #     client.benchmark body: {
      #       name: 'my_benchmark',
      #       competitors: [
      #         {
      #           name: 'query_1',
      #           requests: [
      #             { query: { match: { _all: 'a*' } } }
      #           ]
      #         }
      #       ]
      #     }
      #
      # @example Return statistics for a set of "competing" queries
      #
      #     client.benchmark body: {
      #       name: 'my_benchmark',
      #       competitors: [
      #         {
      #           name: 'query_a',
      #           requests: [
      #             { query: { match: { _all: 'a*' } } }
      #           ]
      #         },
      #         {
      #           name: 'query_b',
      #           requests: [
      #             { query: { match: { _all: 'b*' } } }
      #           ]
      #         }
      #       ]
      #     }
      #
      # @option arguments [List] :index A comma-separated list of index names; use `_all` or empty string
      #                                 to perform the operation on all indices
      # @option arguments [String] :type The name of the document type
      # @option arguments [Hash] :body The search definition using the Query DSL
      # @option arguments [Boolean] :verbose Specify whether to return verbose statistics about each iteration
      #                                      (default: false)
      #
      # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/master/search-benchmark.html
      #
      def benchmark(arguments={})
        valid_params = [
          :verbose ]
        method = HTTP_PUT
        path   = "_bench"
        params = Utils.__validate_and_extract_params arguments, valid_params
        body   = arguments[:body]

        perform_request(method, path, params, body).body
      end
    end
  end
end
