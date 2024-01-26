require 'time'

module Loki
    class Batch
        attr_reader :streams
        def initialize(e)
            @bytes = 0
            @createdAt = Time.now
            @streams = {}
            add(e)
        end

        def size_bytes
            return @bytes
        end

        def add(e)
            @bytes = @bytes + e.entry['line'].length

            # Append the entry to an already existing stream (if any)
            labels = e.labels.sort.to_h
            labelkey = labels.to_s
            if @streams.has_key?(labelkey)
                stream = @streams[labelkey]
                stream['entries'].append(e.entry)
                return
            else
                # Add the entry as a new stream
                @streams[labelkey] = {
                    "labels" => labels,
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

        def to_json
            streams = []
            @streams.each { |_ , stream|
                streams.append(build_stream(stream))
            }
            return {"streams"=>streams}.to_json
        end

        def build_stream(stream)
            values = []
            stream['entries'].each { |entry|
                if entry.key?('metadata')
                    sorted_metadata = entry['metadata'].sort.to_h
                    values.append([
                        entry['ts'].to_s,
                        entry['line'],
                        sorted_metadata
                    ])
                else
                    values.append([
                        entry['ts'].to_s,
                        entry['line']
                    ])
                end
            }
            return {
                'stream'=>stream['labels'],
                'values' => values
            }
        end
    end
end