module Elasticsearch
  module API
    module Actions

      # Return a list of running benchmarks
      #
      # @example
      #
      #     client.list_benchmarks
      #
      # @option arguments [List] :index A comma-separated list of index names; use `_all` or empty string
      #                                 to perform the operation on all indices
      # @option arguments [String] :type The name of the document type
      #
      # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/master/search-benchmark.html
      #
      def list_benchmarks(arguments={})
        method = HTTP_GET
        path   = "_bench"
        params = {}
        body   = nil

        perform_request(method, path, params, body).body
      end
    end
  end
end
