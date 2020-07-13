module Elasticsearch
  module API
    module Cat
      module Actions

        # Display thread pool statistics across nodes (use the `help` parameter to display a list
        # of avaialable thread pools)
        #
        # @example Display information about all thread pools across nodes
        #
        #     puts client.cat.thread_pool
        #
        # @example Display header names in the output
        #
        #     puts client.cat.thread_pool v: true
        #
        # @example Display information about the indexing thread pool
        #
        #     puts client.cat.thread_pool h: %w(h ip index.active index.size index.queue index.rejected), v: true
        #
        # @example Display information about the indexing and search threads, using the short names
        #
        #     puts client.cat.thread_pool h: 'host,ia,is,iq,ir,sa,ss,sq,sr', v: true
        #
        # @option arguments [Boolean] :full_id Display the complete node ID
        # @option arguments [String] :size The multiplier in which to display values
        #                                  (Options: k, m, g, t, p)
        # @option arguments [List] :thread_pool_patterns A comma-separated list of regular expressions to filter
        #                                                the thread pools in the output
        # @option arguments [List] :h Comma-separated list of column names to display -- see the `help` argument
        # @option arguments [Boolean] :v Display column headers as part of the output
        # @option arguments [List] :s Comma-separated list of column names or column aliases to sort by
        # @option arguments [String] :format The output format. Options: 'text', 'json'; default: 'text'
        # @option arguments [Boolean] :help Return information about headers
        # @option arguments [Boolean] :local Return local information, do not retrieve the state from master node
        #                                    (default: false)
        # @option arguments [Time] :master_timeout Explicit operation timeout for connection to master node
        #
        # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/master/cat-thread-pool.html
        #
        def thread_pool(arguments={})
          valid_params = [
            :full_id,
            :size,
            :local,
            :thread_pool_patterns,
            :master_timeout,
            :h,
            :help,
            :v,
            :s ]

          method = HTTP_GET
          path   = "_cat/thread_pool"
          params = Utils.__validate_and_extract_params arguments, valid_params
          params[:h] = Utils.__listify(params[:h]) if params[:h]
          body   = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
