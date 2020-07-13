module Elasticsearch
  module API
    module Indices
      module Actions

        # "Seal" and index or indices for faster recovery
        #
        # @option arguments [List] :index A comma-separated list of index names;
        #                                 use `_all` or empty string for all indices
        #
        # @see http://www.elastic.co/guide/en/elasticsearch/reference/master/indices-seal.html
        #
        def seal(arguments={})
          method = 'POST'
          path   = Utils.__pathify Utils.__listify(arguments[:index]), '_seal'
          params = {}
          body   = nil

          perform_request(method, path, params, body).body
        end
      end
    end
  end
end
