module Elasticsearch
  module API
    module Actions

      # Efficiently iterate over a large result set.
      #
      # When using `from` and `size` to return a large result sets, performance drops as you "paginate" in the set,
      # and you can't guarantee the consistency when the index is being updated at the same time.
      #
      # The "Scroll" API uses a "point in time" snapshot of the index state, which was created via a "Search" API
      # request specifying the `scroll` parameter.
      #
      # @example A basic example
      #
      #     result = client.search index: 'scrollindex',
      #                            scroll: '5m',
      #                            body: { query: { match: { title: 'test' } }, sort: '_id' }
      #
      #     result = client.scroll scroll: '5m', scroll_id: result['_scroll_id']
      #
      # @example Call the `scroll` API until all the documents are returned
      #
      #     # Index 1,000 documents
      #     client.indices.delete index: 'test'
      #     1_000.times do |i| client.index index: 'test', type: 'test', id: i+1, body: {title: "Test #{i}"} end
      #     client.indices.refresh index: 'test'
      #
      #     # Open the "view" of the index by passing the `scroll` parameter
      #     # Sorting by `_doc` makes the operations faster
      #     r = client.search index: 'test', scroll: '1m', body: {sort: ['_doc']}
      #
      #     # Display the initial results
      #     puts "--- BATCH 0 -------------------------------------------------"
      #     puts r['hits']['hits'].map { |d| d['_source']['title'] }.inspect
      #
      #     # Call the `scroll` API until empty results are returned
      #     while r = client.scroll(scroll_id: r['_scroll_id'], scroll: '5m') and not r['hits']['hits'].empty? do
      #       puts "--- BATCH #{defined?($i) ? $i += 1 : $i = 1} -------------------------------------------------"
      #       puts r['hits']['hits'].map { |d| d['_source']['title'] }.inspect
      #       puts
      #     end
      #
      # @option arguments [String] :scroll_id The scroll ID
      # @option arguments [Hash] :body The scroll ID if not passed by URL or query parameter.
      # @option arguments [Duration] :scroll Specify how long a consistent view of the index
      #                                      should be maintained for scrolled search
      #
      # @see http://www.elasticsearch.org/guide/en/elasticsearch/guide/current/scan-scroll.html#scan-scroll
      # @see https://www.elastic.co/guide/en/elasticsearch/reference/current/search-request-scroll.html
      #
      def scroll(arguments={})
        method = HTTP_GET
        path   = "_search/scroll"
        valid_params = [
          :scroll,
          :scroll_id ]

        params = Utils.__validate_and_extract_params arguments, valid_params
        body   = arguments[:body]

        perform_request(method, path, params, body).body
      end
    end
  end
end
