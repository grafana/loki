module Elasticsearch
  module API
    module Indices
      module Actions

        # Get a list of all aliases, or aliases for a specific index.
        #
        # @example Get a list of all aliases
        #
        #     client.indices.get_aliases
        #
        # @option arguments [List] :index A comma-separated list of index names to filter aliases
        # @option arguments [List] :name A comma-separated list of alias names to filter
        # @option arguments [Time] :timeout Explicit timestamp for the document
        # @option arguments [Boolean] :local Return local information,
        #                                    do not retrieve the state from master node (default: false)
        #
        # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/current/indices-aliases.html
        #
        def get_aliases(arguments={})
          valid_params = [ :timeout, :local ]

          method = HTTP_GET
          path   = Utils.__pathify Utils.__listify(arguments[:index]), '_aliases', Utils.__listify(arguments[:name])

          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
