module Elasticsearch
  module API
    module Snapshot
      module Actions

        # Get information about snapshot repositories or a specific repository
        #
        # @example Get all repositories
        #
        #     client.snapshot.get_repository
        #
        # @example Get a specific repository
        #
        #     client.snapshot.get_repository repository: 'my-backups'
        #
        # @option arguments [List] :repository A comma-separated list of repository names. Leave blank or use `_all`
        #                                      to get a list of repositories
        # @option arguments [Time] :master_timeout Explicit operation timeout for connection to master node
        # @option arguments [Boolean] :local Return local information, do not retrieve the state from master node
        #                                    (default: false)
        # @option arguments [Number,List] :ignore The list of HTTP errors to ignore
        #
        # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/master/modules-snapshots.html
        #
        def get_repository(arguments={})
          valid_params = [
            :master_timeout,
            :local ]

          repository = arguments.delete(:repository)

          method = HTTP_GET
          path   = Utils.__pathify( '_snapshot', Utils.__escape(repository) )

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
