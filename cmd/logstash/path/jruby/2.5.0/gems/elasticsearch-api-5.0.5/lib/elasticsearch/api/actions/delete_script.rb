module Elasticsearch
  module API
    module Actions

      # Remove an indexed script from Elasticsearch
      #
      # @option arguments [String] :id Script ID (*Required*)
      # @option arguments [String] :lang Script language (*Required*)
      # @option arguments [Number] :version Explicit version number for concurrency control
      # @option arguments [String] :version_type Specific version type (options: internal, external, external_gte, force)
      #
      # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/master/modules-scripting.html
      #
      def delete_script(arguments={})
        raise ArgumentError, "Required argument 'id' missing"   unless arguments[:id]
        raise ArgumentError, "Required argument 'lang' missing" unless arguments[:lang]

        valid_params = [
          :version,
          :version_type ]

        method = HTTP_DELETE
        path   = "_scripts/#{arguments.delete(:lang)}/#{arguments[:id]}"
        params = Utils.__validate_and_extract_params arguments, valid_params
        body   = nil

        perform_request(method, path, params, body).body
      end
    end
  end
end
