module Elasticsearch
  module API
    module Indices
      module Actions

        # Perform multiple operation on index aliases in a single request.
        #
        # Pass the `actions` (add, remove) in the `body` argument.
        #
        # @example Add multiple indices to a single alias
        #
        #     client.indices.update_aliases body: {
        #       actions: [
        #         { add: { index: 'logs-2013-06', alias: 'year-2013' } },
        #         { add: { index: 'logs-2013-05', alias: 'year-2013' } }
        #       ]
        #     }
        #
        # @example Swap an alias (atomic operation)
        #
        #     client.indices.update_aliases body: {
        #       actions: [
        #         { remove: { index: 'logs-2013-06', alias: 'current-month' } },
        #         { add:    { index: 'logs-2013-07', alias: 'current-month' } }
        #       ]
        #     }
        #
        # @option arguments [Hash] :body The operations definition and other configuration (*Required*)
        # @option arguments [Time] :timeout Explicit operation timeout
        #
        # @see http://www.elasticsearch.org/guide/reference/api/admin-indices-aliases/
        #
        def update_aliases(arguments={})
          raise ArgumentError, "Required argument 'body' missing" unless arguments[:body]
          valid_params = [ :timeout ]

          method = HTTP_POST
          path   = "_aliases"

          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = arguments[:body]

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
