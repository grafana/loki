module Elasticsearch
  module API
    module Cat
      module Actions

        # Display information about the segments in the shards of an index
        #
        # @example Display information for all indices
        #
        #     puts client.cat.segments
        #
        # @option arguments [List] :index A comma-separated list of index names to limit the returned information
        # @option arguments [String] :bytes The unit in which to display byte values (options: b, k, m, g)
        # @option arguments [List] :h Comma-separated list of column names to display
        # @option arguments [Boolean] :help Return help information
        # @option arguments [Boolean] :v Verbose mode. Display column headers
        # @option arguments [List] :s Comma-separated list of column names or column aliases to sort by
        #
        # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/master/cat-segments.html
        #
        def segments(arguments={})
          valid_params = [
            :bytes,
            :index,
            :h,
            :help,
            :v,
            :s ]
          method = 'GET'
          path   = "_cat/segments"
          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
