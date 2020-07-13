module Elasticsearch
  module API
    module Actions

      # Perform multiple percolate operations in a single request, similar to the {#msearch} API
      #
      # Pass the percolate definitions as header-body pairs in the `:body` argument, as an Array of Hashes.
      #
      # @example Perform two different percolations in a single request
      #
      #     client.mpercolate \
      #         body: [
      #           { percolate: { index: "my-index", type: "my-type" } },
      #           { doc: { message: "foo bar" } },
      #           { percolate: { index: "my-other-index", type: "my-other-type", id: "1" } },
      #           { }
      #         ]
      #
      # @option arguments [String] :index The index of the document being count percolated to use as default
      # @option arguments [String] :type The type of the document being percolated to use as default.
      # @option arguments [Array<Hash>]  The percolate request definitions (header & body pairs) (*Required*)
      # @option arguments [Boolean] :ignore_unavailable Whether specified concrete indices should be ignored when
      #                                                 unavailable (missing or closed)
      # @option arguments [Boolean] :allow_no_indices Whether to ignore if a wildcard indices expression resolves into
      #                                               no concrete indices. (This includes `_all` string or when no
      #                                               indices have been specified)
      # @option arguments [String] :expand_wildcards Whether to expand wildcard expression to concrete indices that are
      #                                              open, closed or both. (options: open, closed)
      #
      # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/master/search-percolate.html
      #
      def mpercolate(arguments={})
        raise ArgumentError, "Required argument 'body' missing" unless arguments[:body]
        valid_params = [
          :ignore_unavailable,
          :allow_no_indices,
          :expand_wildcards,
          :percolate_format ]

        method = HTTP_GET
        path   = "_mpercolate"

        params = Utils.__validate_and_extract_params arguments, valid_params
        body   = arguments[:body]

        case
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
