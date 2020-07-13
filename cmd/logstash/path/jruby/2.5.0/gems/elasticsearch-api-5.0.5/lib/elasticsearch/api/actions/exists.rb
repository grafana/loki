module Elasticsearch
  module API
    module Actions

      # Return true if the specified document exists, false otherwise.
      #
      # @example
      #
      #     client.exists? index: 'myindex', type: 'mytype', id: '1'
      #
      # @option arguments [String] :id The document ID (*Required*)
      # @option arguments [String] :index The name of the index (*Required*)
      # @option arguments [String] :type The type of the document (use `_all` to fetch the first document matching the ID across all types) (*Required*)
      # @option arguments [List] :stored_fields A comma-separated list of stored fields to return in the response
      # @option arguments [String] :parent The ID of the parent document
      # @option arguments [String] :preference Specify the node or shard the operation should be performed on (default: random)
      # @option arguments [Boolean] :realtime Specify whether to perform the operation in realtime or search mode
      # @option arguments [Boolean] :refresh Refresh the shard containing the document before performing the operation
      # @option arguments [String] :routing Specific routing value
      # @option arguments [List] :_source True or false to return the _source field or not, or a list of fields to return
      # @option arguments [List] :_source_exclude A list of fields to exclude from the returned _source field
      # @option arguments [List] :_source_include A list of fields to extract and return from the _source field
      # @option arguments [Number] :version Explicit version number for concurrency control
      # @option arguments [String] :version_type Specific version type (options: internal, external, external_gte, force)

      #
      # @see http://elasticsearch.org/guide/reference/api/get/
      #
      def exists(arguments={})
        raise ArgumentError, "Required argument 'id' missing"    unless arguments[:id]
        raise ArgumentError, "Required argument 'index' missing" unless arguments[:index]
        arguments[:type] ||= UNDERSCORE_ALL

        valid_params = [
          :stored_fields,
          :parent,
          :preference,
          :realtime,
          :refresh,
          :routing,
          :_source,
          :_source_exclude,
          :_source_include,
          :version,
          :version_type ]

        method = HTTP_HEAD
        path   = Utils.__pathify Utils.__escape(arguments[:index]),
                                 Utils.__escape(arguments[:type]),
                                 Utils.__escape(arguments[:id])

        params = Utils.__validate_and_extract_params arguments, valid_params
        body   = nil

        Utils.__rescue_from_not_found do
          perform_request(method, path, params, body).status == 200 ? true : false
        end
      end

      alias_method :exists?, :exists
    end
  end
end
