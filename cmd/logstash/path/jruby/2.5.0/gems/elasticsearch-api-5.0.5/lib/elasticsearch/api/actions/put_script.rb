module Elasticsearch
  module API
    module Actions

      # Store a script in an internal index (`.scripts`), to be able to reference them
      # in search definitions (with dynamic scripting disabled)
      #
      # @example Storing an Mvel script in Elasticsearch and using it in function score
      #
      #     client.put_script lang: 'groovy', id: 'my_score', body: { script: 'log(_score * factor)' }
      #
      #     client.search body: {
      #       query: {
      #         function_score: {
      #           query: { match: { title: 'foo' } },
      #           functions: [ { script_score: { script_id: 'my_score', params: { factor: 3 } } } ]
      #         }
      #       }
      #     }
      #
      # @option arguments [String] :id Script ID (*Required*)
      # @option arguments [String] :lang Script language (*Required*)
      # @option arguments [Hash]   :body A JSON document containing the script (*Required*)
      # @option arguments [Number] :version Explicit version number for concurrency control
      # @option arguments [String] :version_type Specific version type (options: internal, external, external_gte, force)
      # @option arguments [String] :op_type Explicit operation type (options: index, create)
      #
      # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/master/modules-scripting.html#_indexed_scripts
      #
      def put_script(arguments={})
        raise ArgumentError, "Required argument 'id' missing"   unless arguments[:id]
        raise ArgumentError, "Required argument 'lang' missing" unless arguments[:lang]
        raise ArgumentError, "Required argument 'body' missing" unless arguments[:body]

        valid_params = [
          :op_type,
          :version,
          :version_type ]

        method = HTTP_PUT
        path   = "_scripts/#{arguments.delete(:lang)}/#{arguments[:id]}"

        params = Utils.__validate_and_extract_params arguments, valid_params

        body   = arguments[:body]

        perform_request(method, path, params, body).body
      end
    end
  end
end
