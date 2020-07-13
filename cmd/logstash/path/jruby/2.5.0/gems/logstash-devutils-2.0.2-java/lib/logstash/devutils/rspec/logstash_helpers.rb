require "logstash/agent"
require "logstash/event"
require "logstash/test_pipeline"

require "stud/try"
require "rspec/expectations"

require "logstash/environment"

module LogStashHelper

  @@excluded_tags = {
      :integration => true,
      :redis => true,
      :socket => true,
      :performance => true,
      :couchdb => true,
      :elasticsearch => true,
      :elasticsearch_secure => true,
      :export_cypher => true
  }

  if LogStash::Environment.windows?
    @@excluded_tags[:unix] = true
  else
    @@excluded_tags[:windows] = true
  end

  def self.excluded_tags
    @@excluded_tags
  end

  TestPipeline = LogStash::TestPipeline

  DEFAULT_NUMBER_OF_TRY = 5
  DEFAULT_EXCEPTIONS_FOR_TRY = [RSpec::Expectations::ExpectationNotMetError]

  def try(number_of_try = DEFAULT_NUMBER_OF_TRY, &block)
    Stud.try(number_of_try.times, DEFAULT_EXCEPTIONS_FOR_TRY, &block)
  end

  def config(configstr)
    let(:config) { configstr }
  end # def config

  def type(default_type)
    deprecated "type(#{default_type.inspect}) no longer has any effect"
  end

  def tags(*tags)
    let(:default_tags) { tags }
    deprecated "tags(#{tags.inspect}) - let(:default_tags) are not used"
  end

  def sample(sample_event, &block)
    name = sample_event.is_a?(String) ? sample_event : LogStash::Json.dump(sample_event)
    name = name[0..50] + "..." if name.length > 50

    describe "\"#{name}\"" do
      let(:pipeline) { new_pipeline_from_string(config) }
      let(:event) do
        sample_event = [sample_event] unless sample_event.is_a?(Array)
        next sample_event.collect do |e|
          e = { "message" => e } if e.is_a?(String)
          next LogStash::Event.new(e)
        end
      end

      let(:results) do
        pipeline.filters.each(&:register)

        pipeline.run_with(event)

        # flush makes sure to empty any buffered events in the filter
        pipeline.flush_filters(:final => true) { |flushed_event| results << flushed_event }

        pipeline.filter_queue_client.processed_events
      end

      # starting at logstash-core 5.3 an initialized pipeline need to be closed
      after do
        pipeline.close if pipeline.respond_to?(:close)
      end

      subject { results.length > 1 ? results : results.first }

      it("when processed", &block)
    end
  end # def sample

  org.logstash.config.ir.compiler.OutputStrategyExt::SimpleAbstractOutputStrategyExt.class_eval do
    field_reader :output # available since LS 6.3
  end

  def input(config_string, test_sink: {}, &block); require 'logstash/outputs/test_sink'
    config_parts = [ config_source(config_string), test_sink_output_source(**test_sink) ]

    pipeline = new_pipeline(config_parts)

    output_delegator = pipeline.outputs.last # LogStash::OutputDelegator
    fail('test_sink output expected') unless output_delegator.config_name.eql?('test_sink')
    test_sink_output = output_delegator.strategy.to_java.output
    queue = test_sink_output.init_event_store

    start_thread = pipeline.start_and_wait

    # NOTE: we used to pass a Queue here, now its a Java List/Queue collection
    result = block.call(pipeline, queue)

    pipeline.shutdown
    start_thread.join if start_thread.alive?

    result
  end

  # @deprecated
  def plugin_input(plugin, &block)
    raise NotImplementedError.new("#{__method__} no longer supported; please refactor")
  end

  def agent(&block)
    it("agent(#{caller[0].gsub(/ .*/, "")}) runs") do
      pipeline = new_pipeline_from_string(config)
      pipeline.run
      block.call
    end
  end

  def new_pipeline_from_string(config_string, pipeline_id: :main, test_sink: {})
    config_parts = [ config_source(config_string) ]

    # include a default test_sink output if no outputs given -> we're using it to track processed events
    # NOTE: a output is required with the JavaPipeline otherwise no processing happen (despite filters being defined)
    if !OUTPUT_BLOCK_RE.match(config_string)
      config_parts << test_sink_output_source(**test_sink)
    elsif test_sink && !test_sink.empty?
      warn "#{__method__} test_sink: #{test_sink.inspect} options have no effect as config_string has an output"
    end

    if !INPUT_BLOCK_RE.match(config_string)
      # NOTE: currently using manual batch to push events down the pipe, so an input isn't required
    end

    new_pipeline(config_parts, pipeline_id)
  end

  def new_pipeline(config_parts, pipeline_id = :main, settings = ::LogStash::SETTINGS.clone)
    pipeline_config = LogStash::Config::PipelineConfig.new(LogStash::Config::Source::Local, pipeline_id, config_parts, settings)
    TestPipeline.new(pipeline_config)
  end

  OUTPUT_BLOCK_RE = defined?(LogStash::Config::Source::Local::ConfigStringLoader::OUTPUT_BLOCK_RE) ?
                        LogStash::Config::Source::Local::ConfigStringLoader::OUTPUT_BLOCK_RE : /output *{/
  private_constant :OUTPUT_BLOCK_RE


  INPUT_BLOCK_RE = defined?(LogStash::Config::Source::Local::ConfigStringLoader::INPUT_BLOCK_RE) ?
                        LogStash::Config::Source::Local::ConfigStringLoader::INPUT_BLOCK_RE : /input *{/
  private_constant :INPUT_BLOCK_RE

  private

  def config_source(config_string)
    org.logstash.common.SourceWithMetadata.new("string", "config_string", config_string)
  end

  def test_sink_output_source(**config)
    config = { id: current_spec_id }.merge(config).map { |k, v| "#{k} => #{v.is_a?(String) ? v.inspect : v}" }.join(' ')
    output_string = "output { test_sink { #{config} } }" # TODO opts for performance store_events: false
    org.logstash.common.SourceWithMetadata.new("string", "test_sink_output", output_string)
  end

  def current_spec_id
    @__current_example_metadata&.[](:location) || 'spec-sample'
  end

  if RUBY_VERSION > '2.5'
    def deprecated(msg)
      Kernel.warn(msg, uplevel: 1)
    end
  else # due JRuby 9.1 (Ruby 2.3)
    def deprecated(msg)
      loc = caller_locations[1]
      Kernel.warn("#{loc.path}:#{loc.lineno}: warning: #{msg}")
    end
  end

end # module LogStash

