module Elasticsearch
  module API
    module Actions

      # Perform multiple search operations in a single request.
      #
      # Pass the search definitions in the `:body` argument
      #
      # @example Perform multiple different searches as `:search`
      #
      #     client.msearch \
      #       body: [
      #         { search: { query: { match_all: {} } } },
      #         { index: 'myindex', type: 'mytype', search: { query: { query_string: { query: '"Test 1"' } } } },
      #         { search_type: 'count', search: { aggregations: { published: { terms: { field: 'published' } } } } }
      #       ]
      #
      # @example Perform multiple different searches as an array of meta/data pairs
      #
      #     client.msearch \
      #       body: [
      #         { query: { match_all: {} } },
      #         { index: 'myindex', type: 'mytype' },
      #         { query: { query_string: { query: '"Test 1"' } } },
      #         { search_type: 'count' },
      #         { aggregations: { published: { terms: { field: 'published' } } } }
      #       ]
      #
      # @option arguments [List] :index A comma-separated list of index names to use as default
      # @option arguments [List] :type A comma-separated list of document types to use as default
      # @option arguments [Hash] :body The request definitions (metadata-search request definition pairs), separated by newlines (*Required*)
      # @option arguments [String] :search_type Search operation type (options: query_then_fetch, query_and_fetch, dfs_query_then_fetch, dfs_query_and_fetch)
      # @option arguments [Number] :max_concurrent_searches Controls the maximum number of concurrent searches the multi search api will execute
      # @option arguments [Boolean] :typed_keys Specify whether aggregation and suggester names should be prefixed by their respective types in the response
      #
      # @see https://www.elastic.co/guide/en/elasticsearch/reference/5.x/search-multi-search.html
      #
      def msearch(arguments={})
        raise ArgumentError, "Required argument 'body' missing" unless arguments[:body]

         valid_params = [
          :search_type,
          :max_concurrent_searches,
          :typed_keys ]

        method = HTTP_GET
        path   = Utils.__pathify( Utils.__listify(arguments[:index]), Utils.__listify(arguments[:type]), '_msearch' )

        params = Utils.__validate_and_extract_params arguments, valid_params
        body   = arguments[:body]

        case
        when body.is_a?(Array) && body.any? { |d| d.has_key? :search }
          payload = body.
            inject([]) do |sum, item|
              meta = item
              data = meta.delete(:search)

              sum << meta
              sum << data
              sum
            end.
            map { |item| Elasticsearch::API.serializer.dump(item) }
          payload << "" unless payload.empty?
          payload = payload.join("\n")
        when body.is_a?(Array)
          payload = body.map { |d| d.is_a?(String) ? d : Elasticsearch::API.serializer.dump(d) }
          payload << "" unless payload.empty?
          payload = payload.join("\n")
        else
          payload = body
        end

        perform_request(method, path, params, payload).body
      end
    end
  end
end
