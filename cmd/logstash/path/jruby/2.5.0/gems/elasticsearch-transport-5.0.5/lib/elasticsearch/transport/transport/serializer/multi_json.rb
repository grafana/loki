module Elasticsearch
  module Transport
    module Transport
      module Serializer

        # An abstract class for implementing serializer implementations
        #
        module Base
          # @param transport [Object] The instance of transport which uses this serializer
          #
          def initialize(transport=nil)
            @transport = transport
          end
        end

        # A default JSON serializer (using [MultiJSON](http://rubygems.org/gems/multi_json))
        #
        class MultiJson
          include Base

          # De-serialize a Hash from JSON string
          #
          def load(string, options={})
            ::MultiJson.load(string, options)
          end

          # Serialize a Hash to JSON string
          #
          def dump(object, options={})
            ::MultiJson.dump(object, options)
          end
        end
      end
    end
  end
end
