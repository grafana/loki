require 'time'

module LogStash
    module Outputs
        class Loki 
            class Batch
                attr_reader :streams
                def initialize(e)
                    @bytes = 0
                    @createdAt = Time.now
                    @streams = {}
                    add(e)
                end 

                def size_bytes()
                    return @bytes
                end

                def add(e)
                    @bytes = @bytes + e.entry['line'].length

                    # Append the entry to an already existing stream (if any)
                    labels = e.labels.to_s
                    if @streams.has_key?(labels)
                        stream = @streams[labels]
                        stream['entries'] = stream['entries'] + e.entry
                        return
                    else  
                        # Add the entry as a new stream
                        @streams[labels] = {
                            "labels" => e.labels,
                            "entries" => [e.entry],
                        }
                    end
                end 

                def size_bytes_after(line)
                    return @bytes + line.length
                end 

                def age()
                    return Time.now - @createdAt
                end
            end 
        end 
    end 
end 