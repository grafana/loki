require_relative 'conversion_helpers'
# Converts any message to loki format
module GenericToLoki
  def self.generic_to_loki(log, extra_labels, chunk)
    # log.debug("GenericToLoki: converting #{chunk}")
    streams = ConversionHelpers.chunk_to_loki(log, chunk)
    payload = ConversionHelpers.payload_builder(log, extra_labels, streams)
    payload
  end
end
