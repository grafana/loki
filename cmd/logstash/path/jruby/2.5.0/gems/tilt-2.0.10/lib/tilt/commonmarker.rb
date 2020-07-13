require 'tilt/template'
require 'commonmarker'

module Tilt
  class CommonMarkerTemplate < Template
    self.default_mime_type = 'text/html'

    OPTION_ALIAS = {
      :smartypants => :SMART
    }
    PARSE_OPTIONS = [
      :SMART,
      :smartypants,
    ].freeze
    RENDER_OPTIONS = [
      :GITHUB_PRE_LANG,
      :HARDBREAKS,
      :NOBREAKS,
      :SAFE,
      :SOURCEPOS,
    ].freeze
    EXTENSIONS = [
      :autolink,
      :strikethrough,
      :table,
      :tagfilter,
    ].freeze

    def extensions
      EXTENSIONS.select do |extension|
        options[extension]
      end
    end

    def parse_options
      raw_options = PARSE_OPTIONS.select do |option|
        options[option]
      end
      actual_options = raw_options.map do |option|
        OPTION_ALIAS[option] || option
      end

      if actual_options.any?
        actual_options
      else
        :DEFAULT
      end
    end

    def render_options
      raw_options = RENDER_OPTIONS.select do |option|
        options[option]
      end
      actual_options = raw_options.map do |option|
        OPTION_ALIAS[option] || option
      end
      if actual_options.any?
        actual_options
      else
        :DEFAULT
      end
    end

    def prepare
      @engine = nil
      @output = nil
    end

    def evaluate(scope, locals, &block)
      doc = CommonMarker.render_doc(data, parse_options, extensions)
      doc.to_html(render_options, extensions)
    end

    def allows_script?
      false
    end
  end
end
