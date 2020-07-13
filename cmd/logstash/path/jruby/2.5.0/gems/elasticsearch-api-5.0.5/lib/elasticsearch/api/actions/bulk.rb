module Elasticsearch
  module API
    module Actions

      # Perform multiple index, delete or update operations in a single request.
      #
      # Supports various different formats of the payload: Array of Strings, Header/Data pairs,
      # or the conveniency "combined" format where data is passed along with the header
      # in a single item in a custom `:data` key.
      #
      # @example Perform three operations in a single request, passing actions and data as an array of hashes
      #
      #     client.bulk body: [
      #       { index: { _index: 'myindex', _type: 'mytype', _id: 1 } },
      #       { title: 'foo' },
      #
      #       { index: { _index: 'myindex', _type: 'mytype', _id: 2 } },
      #       { title: 'foo' },
      #
      #       { delete: { _index: 'myindex', _type: 'mytype', _id: 3  } }
      #     ]
      # @example Perform three operations in a single request, passing data in the `:data` option
      #
      #     client.bulk body: [
      #       { index:  { _index: 'myindex', _type: 'mytype', _id: 1, data: { title: 'foo' } } },
      #       { update: { _index: 'myindex', _type: 'mytype', _id: 2, data: { doc: { title: 'foo' } } } },
      #       { delete: { _index: 'myindex', _type: 'mytype', _id: 3  } }
      #     ]
      #
      # @example Perform a script-based bulk update, passing scripts in the `:data` option
      #
      #     client.bulk body: [
      #       { update: { _index: 'myindex', _type: 'mytype', _id: 1,
      #                  data: {
      #                   script: "ctx._source.counter += value",
      #                   lang: 'js',
      #                   params: { value: 1 }, upsert: { counter: 0 } }
      #                 }},
      #       { update: { _index: 'myindex', _type: 'mytype', _id: 2,
      #                  data: {
      #                   script: "ctx._source.counter += value",
      #                   lang: 'js',
      #                   params: { value: 42 }, upsert: { counter: 0 } }
      #                  }}
      #
      #     ]
      #
      # @option arguments [String] :index Default index for items which don't provide one
      # @option arguments [String] :type Default document type for items which don't provide one
      # @option arguments [Hash] :body The operation definition and data (action-data pairs), separated by newlines (*Required*)
      # @option arguments [String] :wait_for_active_shards Sets the number of shard copies that must be active before proceeding with the bulk operation. Defaults to 1, meaning the primary shard only. Set to `all` for all shard copies, otherwise set to any non-negative value less than or equal to the total number of copies for the shard (number of replicas + 1)
      # @option arguments [String] :refresh If `true` then refresh the effected shards to make this operation visible to search, if `wait_for` then wait for a refresh to make this operation visible to search, if `false` (the default) then do nothing with refreshes. (options: true, false, wait_for)
      # @option arguments [String] :routing Specific routing value
      # @option arguments [Time] :timeout Explicit operation timeout
      # @option arguments [String] :type Default document type for items which don't provide one
      # @option arguments [List] :fields Default comma-separated list of fields to return in the response for updates, can be overridden on each sub-request
      # @option arguments [List] :_source True or false to return the _source field or not, or default list of fields to return, can be overridden on each sub-request
      # @option arguments [List] :_source_exclude Default list of fields to exclude from the returned _source field, can be overridden on each sub-request
      # @option arguments [List] :_source_include Default list of fields to extract and return from the _source field, can be overridden on each sub-request
      # @option arguments [String] :pipeline The pipeline ID to preprocess incoming documents with
      #
      # @return [Hash] Deserialized Elasticsearch response
      #
      # @see https://www.elastic.co/guide/en/elasticsearch/reference/current/docs-bulk.html
      #
      def bulk(arguments={})
        arguments = arguments.clone

        type      = arguments.delete(:type)

        valid_params = [
          :wait_for_active_shards,
          :refresh,
          :routing,
          :timeout,
          :type,
          :fields,
          :_source,
          :_source_exclude,
          :_source_include,
          :pipeline ]

        method = HTTP_POST
        path   = Utils.__pathify Utils.__escape(arguments[:index]), Utils.__escape(type), '_bulk'

        params = Utils.__validate_and_extract_params arguments, valid_params
        body   = arguments[:body]

        if body.is_a? Array
          payload = Utils.__bulkify(body)
        else
          payload = body
        end

        perform_request(method, path, params, payload).body
      end
    end
  end
end
