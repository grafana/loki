module LogStash
    module Outputs
        class Loki 
            class Entry
                attr_reader :labels, :entry
                def initialize(labels, entry)
                    @labels   = labels
                    @entry = entry
                end 
            end 
        end 
    end 
end 