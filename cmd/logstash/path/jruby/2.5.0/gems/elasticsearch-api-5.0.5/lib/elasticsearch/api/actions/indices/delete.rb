module Elasticsearch
  module API
    module Indices
      module Actions

        # Delete an index, list of indices, or all indices in the cluster.
        #
        # @example Delete an index
        #
        #     client.indices.delete index: 'foo'
        #
        # @example Delete a list of indices
        #
        #     client.indices.delete index: ['foo', 'bar']
        #     client.indices.delete index: 'foo,bar'
        #
        #
        # @example Delete a list of indices matching wildcard expression
        #
        #     client.indices.delete index: 'foo*'
        #
        # @example Delete all indices
        #
        #     client.indices.delete index: '_all'
        #
        # @option arguments [List] :index A comma-separated list of indices to delete;
        #                                 use `_all` to delete all indices
        # @option arguments [Time] :timeout Explicit operation timeout
        #
        # @see http://www.elasticsearch.org/guide/reference/api/admin-indices-delete-index/
        #
        def delete(arguments={})
          valid_params = [ :timeout ]

          method = HTTP_DELETE
          path   = Utils.__pathify Utils.__listify(arguments[:index])

          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = nil

          if Array(arguments[:ignore]).include?(404)
            Utils.__rescue_from_not_found { perform_request(method, path, params, body).body }
          else
            perform_request(method, path, params, body).body
          end
        end
      end
    end
  end
end
