module Loki
    def to_ns(s)
        (s.to_f * (10**9)).to_i
    end
    class Entry
        include Loki
        attr_reader :labels, :entry
        def initialize(event,message_field,include_fields,metadata_fields)
            @entry = {
                "ts" => to_ns(event.get("@timestamp")),
                "line" => event.get(message_field).to_s
            }
            event = event.clone()
            event.remove(message_field)
            event.remove("@timestamp")

            @labels = {}
            event.to_hash.each { |key,value|
                next if key.start_with?('@')
                next if value.is_a?(Hash)
                next if include_fields.length() > 0 and not include_fields.include?(key)
                @labels[key] = value.to_s
            }

            # Unlike include_fields we should skip if no metadata_fields provided
            if metadata_fields.length() > 0
                @metadata = {}
                event.to_hash.each { |key,value|
                    next if key.start_with?('@')
                    next if value.is_a?(Hash)
                    next if metadata_fields.length() > 0 and not metadata_fields.include?(key)
                    @metadata[key] = value.to_s
                }

                # Add @metadata to @entry if there was a match
                if @metadata.size > 0
                    @entry.merge!('metadata' => @metadata)
                end
            end
        end
    end
end
