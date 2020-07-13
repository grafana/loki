module Elasticsearch
  module API
    module Actions

      # Return the capabilities of fields among multiple indices
      #
      # @example
      #     client.field_caps fields: '*'
      #     # => { "fields" => "t"=>{"text"=>{"type"=>"text", "searchable"=>true, "aggregatable"=>false}} ...
      #
      # @option arguments [List] :index A comma-separated list of index names; use `_all` or empty string to perform the operation on all indices
      # @option arguments [Hash] :body Field json objects containing an array of field names
      # @option arguments [List] :fields A comma-separated list of field names (*Required*)
      # @option arguments [Boolean] :ignore_unavailable Whether specified concrete indices should be ignored when unavailable (missing or closed)
      # @option arguments [Boolean] :allow_no_indices Whether to ignore if a wildcard indices expression resolves into no concrete indices. (This includes `_all` string or when no indices have been specified)
      # @option arguments [String] :expand_wildcards Whether to expand wildcard expression to concrete indices that are open, closed or both. (options: open, closed, none, all)
      #
      # @see http://www.elastic.co/guide/en/elasticsearch/reference/master/search-field-caps.html
      #
      def field_caps(arguments={})
        raise ArgumentError, "Required argument 'fields' missing" unless arguments[:fields]

        valid_params = [
          :fields,
          :ignore_unavailable,
          :allow_no_indices,
          :expand_wildcards ]

        method = HTTP_GET
        path   = Utils.__pathify Utils.__listify(arguments[:index]), '_field_caps'
        params = Utils.__validate_and_extract_params arguments, valid_params
        body   = arguments[:body]

        perform_request(method, path, params, body).body
      end
    end
  end
end
