module Elasticsearch
  module API
    module Cat
      module Actions; end

      # Client for the "cat" namespace (includes the {Cat::Actions} methods)
      #
      class CatClient
        include Common::Client, Common::Client::Base, Cat::Actions
      end

      # Proxy method for {CatClient}, available in the receiving object
      #
      def cat
        @cat ||= CatClient.new(self)
      end

    end
  end
end
