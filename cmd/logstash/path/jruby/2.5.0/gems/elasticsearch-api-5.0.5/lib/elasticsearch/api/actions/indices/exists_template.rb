module Elasticsearch
  module API
    module Indices
      module Actions

        # Return true if the specified index template exists, false otherwise.
        #
        #     client.indices.exists_template? name: 'mytemplate'
        #
        # @option arguments [String] :name The name of the template (*Required*)
        # @option arguments [Boolean] :local Return local information, do not retrieve the state from master node (default: false)
        # @option arguments [Time] :master_timeout Explicit operation timeout for connection to master node
        #
        # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/master/indices-templates.html
        #
        def exists_template(arguments={})
          raise ArgumentError, "Required argument 'name' missing" unless arguments[:name]
          valid_params = [ :local, :master_timeout ]

          method = HTTP_HEAD
          path   = Utils.__pathify '_template', Utils.__escape(arguments[:name])

          params = Utils.__validate_and_extract_params arguments, valid_params
          body = nil

          Utils.__rescue_from_not_found do
            perform_request(method, path, params, body).status == 200 ? true : false
          end
        end

        alias_method :exists_template?, :exists_template
      end
    end
  end
end
