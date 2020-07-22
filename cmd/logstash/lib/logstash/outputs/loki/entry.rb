module Loki
    def to_ns(s)
        (s.to_f * (10**9)).to_i
    end
    class Entry
        include Loki
        attr_reader :labels, :entry
        def initialize(event,message_field)
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
                @labels[key] = value.to_s
            }
        end
    end
end
