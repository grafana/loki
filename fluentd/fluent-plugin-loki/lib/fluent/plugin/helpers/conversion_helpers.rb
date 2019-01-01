#!/usr/bin/env ruby
#
#
require 'time'
# Converts chunks to loki format
module ConversionHelpers
  def self.numeric?(val)
    !Float(val).nil?
  rescue StandardError
    false
  end

  def self.labels_to_protocol(extra_labels, data_labels)
    formatted_labels = []
    unless extra_labels.nil?
      extra_labels.each do |k, v|
        formatted_labels.push("#{k}=\"#{v}\"")
      end
    end
    unless data_labels.nil?
      # puts "ConversionHelpers.labels_to_protocol data_labels = #{data_labels}"
      data_labels.each do |k, v|
        formatted_labels.push("#{k}=\"#{v}\"")
      end
    end
    '{' + formatted_labels.join(',') + '}'
  end

  def self.payload_builder(_log, extra_labels, streams)
    payload = []
    # log.info("ConversionHelpers.payload_builder: extra labels #{extra_labels}")
    streams.each do |k, v|
      # log.info("ConversionHelpers.payload_builder: stream_labels #{k}")
      # create a stream for each label set.
      # Additionally sort the entries by timestamp just incase we
      # got them out of order.
      # 'labels' => '{worker="0"}',
      payload.push(
        'labels' => labels_to_protocol(extra_labels, k),
        'entries' => v.sort_by { |hsh| hsh[:ts] }
      )
    end
    payload
  end

  # convert a line to loki line with labels
  def self.line_to_loki(_log, record)
    chunk_labels = {}
    line = ''
    if record.is_a?(Hash)
      if record.key?('message')
        line = record['message']
        record.each do |k, v|
          # puts "ConversionHelpers.line_to_loki key = #{k} value = #{v}"
          chunk_labels[k] = v if k != 'message'
        end
      end
    else
      line = record.to_s
    end
    # puts "ConversionHelpers.line_to_loki: chunk_labels = #{chunk_labels}"
    # return both the line content plus the labels found in the record
    {
      line: line,
      labels: chunk_labels
    }
  end

  # iterate through each chunk.  If the record has a "message" field then we use
  # that as the "line" for the Loki stream entry.
  # Else the just set "line" to the stringified record.
  def self.chunk_to_loki(log, chunk)
    streams = {}
    last_time = nil
    chunk.each do |time, record|
      # each chunk has a unique set of labels
      last_time = time if last_time.nil?
      result = line_to_loki(log, record)
      chunk_labels = result[:labels]
      # initialize a new stream with the chunk_labels if it does not exist
      streams[chunk_labels] = [] if streams[chunk_labels].nil?
      # NOTE: timestamp must include nanoseconds
      # append to matching chunk_labels key
      streams[chunk_labels].push(
        'ts' => Time.at(time.to_f).iso8601(6),
        'line' => result[:line]
      )
    end
    streams
  end
end
