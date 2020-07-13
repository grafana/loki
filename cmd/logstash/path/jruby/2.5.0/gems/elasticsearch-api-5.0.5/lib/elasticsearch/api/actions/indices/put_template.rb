module Elasticsearch
  module API
    module Indices
      module Actions

        # Create or update an index template.
        #
        # @example Create a template for all indices starting with `logs-`
        #
        #     client.indices.put_template name: 'foo',
        #                                 body: { template: 'logs-*', settings: { 'index.number_of_shards' => 1 } }
        #
        # @option arguments [String] :name The name of the template (*Required*)
        # @option arguments [Hash] :body The template definition (*Required*)
        # @option arguments [Boolean] :create Whether the index template should only be added for a new one, #                                     or can also replace an existing one (default: false)
        # @option arguments [Number] :order The order for this template when merging multiple matching ones
        #                                   (higher numbers are merged later, overriding the lower numbers)
        # @option arguments [Time] :timeout Explicit operation timeout
        # @option arguments [Time] :master_timeout Specify timeout for connection to master
        # @option arguments [Boolean] :flat_settings Return settings in flat format (default: false)
        #
        # @see http://www.elasticsearch.org/guide/reference/api/admin-indices-templates/
        #
        def put_template(arguments={})
          raise ArgumentError, "Required argument 'name' missing" unless arguments[:name]
          raise ArgumentError, "Required argument 'body' missing" unless arguments[:body]
          valid_params = [ :create, :order, :timeout ]

          method = HTTP_PUT
          path   = Utils.__pathify '_template', Utils.__escape(arguments[:name])

          params = Utils.__validate_and_extract_params arguments, valid_params
          body   = arguments[:body]

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
