module Elasticsearch
  module API
    module Ingest
      module Actions; end

      # Client for the "ingest" namespace (includes the {Ingest::Actions} methods)
      #
      class IngestClient
        include Common::Client, Common::Client::Base, Ingest::Actions
      end

      # Proxy method for {IngestClient}, available in the receiving object
      #
      def ingest
        @ingest ||= IngestClient.new(self)
      end

    end
  end
end
