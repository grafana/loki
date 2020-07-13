module Elasticsearch
  module API
    module Indices
      module Actions

        # Retrieve information about one or more indices
        #
        # @option arguments [List] :index A comma-separated list of index names (*Required*)
        # @option arguments [List] :feature A comma-separated list of features
        #                                   (options: _settings, _mappings, _aliases]
        # @option arguments [Boolean] :local Return local information, do not retrieve the state from master node
        #                                    (default: false)
        # @option arguments [Boolean] :ignore_unavailable Ignore unavailable indexes (default: false)
        # @option arguments [Boolean] :allow_no_indices Ignore if a wildcard expression resolves to no concrete
        #                                               indices (default: false)
        # @option arguments [List] :expand_wildcards Whether wildcard expressions should get expanded
        #                                            to open or closed indices (default: open)
        # @option arguments [Boolean] :flat_settings Return settings in flat format (default: false)
        # @option arguments [Boolean] :human Whether to return version and creation date values in
        #                                    human-readable format (default: false)
        # @option arguments [Boolean] :include_defaults Whether to return all default setting
        #                                               for each of the indices (default:false)
        #
        # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/master/indices-get-index.html
        #
        def get(arguments={})
          raise ArgumentError, "Required argument 'index' missing" unless arguments[:index]

          valid_params = [
            :local,
            :ignore_unavailable,
            :allow_no_indices,
            :expand_wildcards,
            :flat_settings,
            :human,
            :include_defaults ]

          method = HTTP_GET

          path   = Utils.__pathify Utils.__listify(arguments[:index]), Utils.__listify(arguments.delete(:feature))

          params = Utils.__validate_and_extract_params arguments, valid_params
          body = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
