module Elasticsearch
  module API
    module Actions

      # Returns information and statistics about terms in the fields of multiple documents
      # in a single request/response. The semantics are similar to the {#mget} API.
      #
      # @example Return information about multiple documents in a specific index
      #
      #     subject.mtermvectors index: 'my-index', type: 'my-type', body: { ids: [1, 2, 3] }
      #
      # @option arguments [String] :index The name of the index
      # @option arguments [String] :type The type of the document
      # @option arguments [Hash] :body Document identifiers; can be either `docs` (containing full document information)
      #                                or `ids` (when index and type is provided in the URL (*Required*)
      # @option arguments [List] :ids A comma-separated list of documents ids (alternative to `:body`)
      # @option arguments [Boolean] :term_statistics Whether total term frequency and
      #                                              document frequency should be returned.
      # @option arguments [Boolean] :field_statistics Whether document count, sum of document frequencies
      #                                               and sum of total term frequencies should be returned.
      # @option arguments [List] :fields A comma-separated list of fields to return
      # @option arguments [Boolean] :offsets Whether term offsets should be returned
      # @option arguments [Boolean] :positions Whether term positions should be returned
      # @option arguments [Boolean] :payloads Whether term payloads should be returned
      # @option arguments [String] :preference Specify the node or shard the operation should be performed on
      #                                        (default: random)
      # @option arguments [String] :realtime Specifies if requests are real-time as opposed to near-real-time
      #                                      (default: true)
      # @option arguments [String] :routing Specific routing value
      # @option arguments [String] :parent Parent ID of documents
      #
      # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/master/docs-multi-termvectors.html
      #
      # @see #mget
      # @see #termvector
      #
      def mtermvectors(arguments={})
        valid_params = [
          :ids,
          :term_statistics,
          :field_statistics,
          :fields,
          :offsets,
          :positions,
          :payloads,
          :preference,
          :realtime,
          :routing,
          :parent ]

        ids = arguments.delete(:ids)

        method = HTTP_GET
        path   = Utils.__pathify Utils.__escape(arguments[:index]),
                                 Utils.__escape(arguments[:type]),
                                 '_mtermvectors'

        params = Utils.__validate_and_extract_params arguments, valid_params

        if ids
          body = { :ids => ids }
        else
          body = arguments[:body]
        end

        perform_request(method, path, params, body).body
      end
    end
  end
end
