module Elasticsearch
  module API
    module Indices
      module Actions

        # Return the settings for all indices, or a list of indices.
        #
        # @example Get settings for all indices
        #
        #     client.indices.get_settings
        #
        # @example Get settings for the 'foo' index
        #
        #     client.indices.get_settings index: 'foo'
        #
        # @example Get settings for indices beginning with foo
        #
        #     client.indices.get_settings prefix: 'foo'
        #
        # @example Get settings for an index named _myindex_
        #
        #     client.indices.get_settings index: 'myindex'
        #
        # @option arguments [List] :index A comma-separated list of index names; use `_all` or empty string
        #                                 to perform the operation on all indices
        # @option arguments [List] :name The name of the settings that should be included in the response
        # @option arguments [String] :prefix The prefix all settings must have in order to be included
        # @option arguments [Boolean] :allow_no_indices Whether to ignore if a wildcard indices expression resolves into
        #                                               no concrete indices. (This includes `_all` string or when no
        #                                               indices have been specified)
        # @option arguments [String] :expand_wildcards Whether to expand wildcard expression to concrete indices that
        #                                              are open, closed or both. (options: open, closed)
        # @option arguments [String] :ignore_indices When performed on multiple indices, allows to ignore
        #                                            `missing` ones (options: none, missing) @until 1.0
        # @option arguments [Boolean] :ignore_unavailable Whether specified concrete indices should be ignored when
        #                                                 unavailable (missing, closed, etc)
        # @option arguments [Boolean] :include_defaults Whether to return all default clusters setting
        # @option arguments [Boolean] :flat_settings Return settings in flat format (default: false)
        # @option arguments [Boolean] :local Return local information, do not retrieve the state from master node
        #                                    (default: false)
        #
        # @see http://www.elasticsearch.org/guide/reference/api/admin-indices-get-settings/
        #
        def get_settings(arguments={})
          valid_params = [
            :prefix,
            :ignore_indices,
            :ignore_unavailable,
            :include_defaults,
            :allow_no_indices,
            :expand_wildcards,
            :flat_settings,
            :local
          ]

          method = HTTP_GET
          path   = Utils.__pathify Utils.__listify(arguments[:index]),
                                   Utils.__listify(arguments[:type]),
                                   arguments.delete(:prefix),
                                   '_settings',
                                   Utils.__escape(arguments[:name])
          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
