module Elasticsearch
  module API
    module Actions

      # Abort a particular scroll search and clear all the resources associated with it.
      #
      # @option arguments [List] :scroll_id A comma-separated list of scroll IDs to clear;
      #                                     use `_all` clear all scroll search contexts
      #
      # @see http://www.elasticsearch.org/guide/en/elasticsearch/reference/current/search-request-search-type.html#clear-scroll
      #
      def clear_scroll(arguments={})
        raise ArgumentError, "Required argument 'scroll_id' missing" unless arguments[:scroll_id]

        method = HTTP_DELETE
        path   = Utils.__pathify '_search/scroll', Utils.__listify(arguments.delete(:scroll_id))
        params = {}
        body   = arguments[:body]

        perform_request(method, path, params, body).body
      end
    end
  end
end
