# -*- coding: utf-8 -*-
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#

require 'yaml'

module Kramdown

  # This module defines all options that are used by parsers and/or converters as well as providing
  # methods to deal with the options.
  module Options

    # Helper class introducing a boolean type for specifying boolean values (+true+ and +false+) as
    # option types.
    class Boolean

      # Return +true+ if +other+ is either +true+ or +false+
      def self.===(other)
        FalseClass === other || TrueClass === other
      end

    end

    # ----------------------------
    # :section: Option definitions
    #
    # This sections describes the methods that can be used on the Options module.
    # ----------------------------

    # Struct class for storing the definition of an option.
    Definition = Struct.new(:name, :type, :default, :desc, :validator)

    # Allowed option types.
    ALLOWED_TYPES = [String, Integer, Float, Symbol, Boolean, Object]

    @options = {}

    # Define a new option called +name+ (a Symbol) with the given +type+ (String, Integer, Float,
    # Symbol, Boolean, Object), default value +default+ and the description +desc+. If a block is
    # specified, it should validate the value and either raise an error or return a valid value.
    #
    # The type 'Object' should only be used for complex types for which none of the other types
    # suffices. A block needs to be specified when using type 'Object' and it has to cope with
    # a value given as string and as the opaque type.
    def self.define(name, type, default, desc, &block)
      name = name.to_sym
      raise ArgumentError, "Option name #{name} is already used" if @options.has_key?(name)
      raise ArgumentError, "Invalid option type #{type} specified" if !ALLOWED_TYPES.include?(type)
      raise ArgumentError, "Invalid type for default value" if !(type === default) && !default.nil?
      raise ArgumentError, "Missing validator block" if type == Object && block.nil?
      @options[name] = Definition.new(name, type, default, desc, block)
    end

    # Return all option definitions.
    def self.definitions
      @options
    end

    # Return +true+ if an option called +name+ is defined.
    def self.defined?(name)
      @options.has_key?(name.to_sym)
    end

    # Return a Hash with the default values for all options.
    def self.defaults
      temp = {}
      @options.each {|n, o| temp[o.name] = o.default}
      temp
    end

    # Merge the #defaults Hash with the *parsed* options from the given Hash, i.e. only valid option
    # names are considered and their value is run through the #parse method.
    def self.merge(hash)
      temp = defaults
      hash.each do |k,v|
        k = k.to_sym
        @options.has_key?(k) ? temp[k] = parse(k, v) : temp[k] = v
      end
      temp
    end

    # Parse the given value +data+ as if it was a value for the option +name+ and return the parsed
    # value with the correct type.
    #
    # If +data+ already has the correct type, it is just returned. Otherwise it is converted to a
    # String and then to the correct type.
    def self.parse(name, data)
      name = name.to_sym
      raise ArgumentError, "No option named #{name} defined" if !@options.has_key?(name)
      if !(@options[name].type === data)
        data = data.to_s
        data = if @options[name].type == String
                 data
               elsif @options[name].type == Integer
                 Integer(data) rescue raise Kramdown::Error, "Invalid integer value for option '#{name}': '#{data}'"
               elsif @options[name].type == Float
                 Float(data) rescue raise Kramdown::Error, "Invalid float value for option '#{name}': '#{data}'"
               elsif @options[name].type == Symbol
                 str_to_sym(data)
               elsif @options[name].type == Boolean
                 data.downcase.strip != 'false' && !data.empty?
               end
      end
      data = @options[name].validator[data] if @options[name].validator
      data
    end

    # Converts the given String +data+ into a Symbol or +nil+ with the
    # following provisions:
    #
    # - A leading colon is stripped from the string.
    # - An empty value or a value equal to "nil" results in +nil+.
    def self.str_to_sym(data)
      data = data.strip
      data = data[1..-1] if data[0] == ?:
      (data.empty? || data == 'nil' ? nil : data.to_sym)
    end

    # ----------------------------
    # :section: Option Validators
    #
    # This sections contains all pre-defined option validators.
    # ----------------------------

    # Ensures that the option value +val+ for the option called +name+ is a valid array. The
    # parameter +val+ can be
    #
    # - a comma separated string which is split into an array of values
    # - or an array.
    #
    # Optionally, the array is checked for the correct size.
    def self.simple_array_validator(val, name, size = nil)
      if String === val
        val = val.split(/,/)
      elsif !(Array === val)
        raise Kramdown::Error, "Invalid type #{val.class} for option #{name}"
      end
      if size && val.size != size
        raise Kramdown::Error, "Option #{name} needs exactly #{size} values"
      end
      val
    end

    # Ensures that the option value +val+ for the option called +name+ is a valid hash. The
    # parameter +val+ can be
    #
    # - a hash in YAML format
    # - or a Ruby Hash object.
    def self.simple_hash_validator(val, name)
      if String === val
        begin
          val = YAML.load(val)
        rescue RuntimeError, ArgumentError, SyntaxError
          raise Kramdown::Error, "Invalid YAML value for option #{name}"
        end
      end
      raise Kramdown::Error, "Invalid type #{val.class} for option #{name}" if !(Hash === val)
      val
    end

    # ----------------------------
    # :section: Option Definitions
    #
    # This sections contains all option definitions that are used by the included
    # parsers/converters.
    # ----------------------------

    define(:template, String, '', <<EOF)
The name of an ERB template file that should be used to wrap the output
or the ERB template itself.

This is used to wrap the output in an environment so that the output can
be used as a stand-alone document. For example, an HTML template would
provide the needed header and body tags so that the whole output is a
valid HTML file. If no template is specified, the output will be just
the converted text.

When resolving the template file, the given template name is used first.
If such a file is not found, the converter extension (the same as the
converter name) is appended. If the file still cannot be found, the
templates name is interpreted as a template name that is provided by
kramdown (without the converter extension). If the file is still not
found, the template name is checked if it starts with 'string://' and if
it does, this prefix is removed and the rest is used as template
content.

kramdown provides a default template named 'document' for each converter.

Default: ''
Used by: all converters
EOF

    define(:auto_ids, Boolean, true, <<EOF)
Use automatic header ID generation

If this option is `true`, ID values for all headers are automatically
generated if no ID is explicitly specified.

Default: true
Used by: HTML/Latex converter
EOF

    define(:auto_id_stripping, Boolean, false, <<EOF)
Strip all formatting from header text for automatic ID generation

If this option is `true`, only the text elements of a header are used
for generating the ID later (in contrast to just using the raw header
text line).

This option will be removed in version 2.0 because this will be the
default then.

Default: false
Used by: kramdown parser
EOF

    define(:auto_id_prefix, String, '', <<EOF)
Prefix used for automatically generated header IDs

This option can be used to set a prefix for the automatically generated
header IDs so that there is no conflict when rendering multiple kramdown
documents into one output file separately. The prefix should only
contain characters that are valid in an ID!

Default: ''
Used by: HTML/Latex converter
EOF

    define(:transliterated_header_ids, Boolean, false, <<EOF)
Transliterate the header text before generating the ID

Only ASCII characters are used in headers IDs. This is not good for
languages with many non-ASCII characters. By enabling this option
the header text is transliterated to ASCII as good as possible so that
the resulting header ID is more useful.

The stringex library needs to be installed for this feature to work!

Default: false
Used by: HTML/Latex converter
EOF

    define(:parse_block_html, Boolean, false, <<EOF)
Process kramdown syntax in block HTML tags

If this option is `true`, the kramdown parser processes the content of
block HTML tags as text containing block-level elements. Since this is
not wanted normally, the default is `false`. It is normally better to
selectively enable kramdown processing via the markdown attribute.

Default: false
Used by: kramdown parser
EOF

    define(:parse_span_html, Boolean, true, <<EOF)
Process kramdown syntax in span HTML tags

If this option is `true`, the kramdown parser processes the content of
span HTML tags as text containing span-level elements.

Default: true
Used by: kramdown parser
EOF

    define(:html_to_native, Boolean, false, <<EOF)
Convert HTML elements to native elements

If this option is `true`, the parser converts HTML elements to native
elements. For example, when parsing `<em>hallo</em>` the emphasis tag
would normally be converted to an `:html` element with tag type `:em`.
If `html_to_native` is `true`, then the emphasis would be converted to a
native `:em` element.

This is useful for converters that cannot deal with HTML elements.

Default: false
Used by: kramdown parser
EOF

    define(:link_defs, Object, {}, <<EOF) do |val|
Pre-defines link definitions

This option can be used to pre-define link definitions. The value needs
to be a Hash where the keys are the link identifiers and the values are
two element Arrays with the link URL and the link title.

If the value is a String, it has to contain a valid YAML hash and the
hash has to follow the above guidelines.

Default: {}
Used by: kramdown parser
EOF
      val = simple_hash_validator(val, :link_defs)
      val.each do |k,v|
        if !(Array === v) || v.size > 2 || v.size < 1
          raise Kramdown::Error, "Invalid structure for hash value of option #{name}"
        end
        v << nil if v.size == 1
      end
      val
    end

    define(:footnote_nr, Integer, 1, <<EOF)
The number of the first footnote

This option can be used to specify the number that is used for the first
footnote.

Default: 1
Used by: HTML converter
EOF

    define(:enable_coderay, Boolean, true, <<EOF)
Use coderay for syntax highlighting

If this option is `true`, coderay is used by the HTML converter for
syntax highlighting the content of code spans and code blocks.

Default: true
Used by: HTML converter
EOF

    define(:coderay_wrap, Symbol, :div, <<EOF)
Defines how the highlighted code should be wrapped

The possible values are :span, :div or nil.

Default: :div
Used by: HTML converter
EOF

    define(:coderay_line_numbers, Symbol, :inline, <<EOF)
Defines how and if line numbers should be shown

The possible values are :table, :inline or nil. If this option is
nil, no line numbers are shown.

Default: :inline
Used by: HTML converter
EOF

    define(:coderay_line_number_start, Integer, 1, <<EOF)
The start value for the line numbers

Default: 1
Used by: HTML converter
EOF

    define(:coderay_tab_width, Integer, 8, <<EOF)
The tab width used in highlighted code

Used by: HTML converter
EOF

    define(:coderay_bold_every, Object, 10, <<EOF) do |val|
Defines how often a line number should be made bold

Can either be an integer or false (to turn off bold line numbers
completely).

Default: 10
Used by: HTML converter
EOF
      if val == false || val.to_s == 'false'
        false
      else
        Integer(val.to_s) rescue raise Kramdown::Error, "Invalid value for option 'coderay_bold_every'"
      end
end

    define(:coderay_css, Symbol, :style, <<EOF)
Defines how the highlighted code gets styled

Possible values are :class (CSS classes are applied to the code
elements, one must supply the needed CSS file) or :style (default CSS
styles are directly applied to the code elements).

Default: style
Used by: HTML converter
EOF

    define(:coderay_default_lang, Symbol, nil, <<EOF)
Sets the default language for highlighting code blocks

If no language is set for a code block, the default language is used
instead. The value has to be one of the languages supported by coderay
or nil if no default language should be used.

Default: nil
Used by: HTML converter
EOF

    define(:entity_output, Symbol, :as_char, <<EOF)
Defines how entities are output

The possible values are :as_input (entities are output in the same
form as found in the input), :numeric (entities are output in numeric
form), :symbolic (entities are output in symbolic form if possible) or
:as_char (entities are output as characters if possible, only available
on Ruby 1.9).

Default: :as_char
Used by: HTML converter, kramdown converter
EOF

    define(:toc_levels, Object, (1..6).to_a, <<EOF) do |val|
Defines the levels that are used for the table of contents

The individual levels can be specified by separating them with commas
(e.g. 1,2,3) or by using the range syntax (e.g. 1..3). Only the
specified levels are used for the table of contents.

Default: 1..6
Used by: HTML/Latex converter
EOF
      case val
      when String
        if val =~ /^(\d)\.\.(\d)$/
          val = Range.new($1.to_i, $2.to_i).to_a
        elsif val =~ /^\d(?:,\d)*$/
          val = val.split(/,/).map {|s| s.to_i}.uniq
        else
          raise Kramdown::Error, "Invalid syntax for option toc_levels"
        end
      when Array, Range
        val = val.map {|s| s.to_i}.uniq
      else
        raise Kramdown::Error, "Invalid type #{val.class} for option toc_levels"
      end
      if val.any? {|i| !(1..6).include?(i)}
        raise Kramdown::Error, "Level numbers for option toc_levels have to be integers from 1 to 6"
      end
      val
    end

    define(:line_width, Integer, 72, <<EOF)
Defines the line width to be used when outputting a document

Default: 72
Used by: kramdown converter
EOF

    define(:latex_headers, Object, %w{section subsection subsubsection paragraph subparagraph subparagraph}, <<EOF) do |val|
Defines the LaTeX commands for different header levels

The commands for the header levels one to six can be specified by
separating them with commas.

Default: section,subsection,subsubsection,paragraph,subparagraph,subparagraph
Used by: Latex converter
EOF
      simple_array_validator(val, :latex_headers, 6)
    end

    define(:smart_quotes, Object, %w{lsquo rsquo ldquo rdquo}, <<EOF) do |val|
Defines the HTML entity names or code points for smart quote output

The entities identified by entity name or code point that should be
used for, in order, a left single quote, a right single quote, a left
double and a right double quote are specified by separating them with
commas.

Default: lsquo,rsquo,ldquo,rdquo
Used by: HTML/Latex converter
EOF
      val = simple_array_validator(val, :smart_quotes, 4)
      val.map! {|v| Integer(v) rescue v}
      val
    end

    define(:remove_block_html_tags, Boolean, true, <<EOF)
Remove block HTML tags

If this option is `true`, the RemoveHtmlTags converter removes
block HTML tags.

Default: true
Used by: RemoveHtmlTags converter
EOF

    define(:remove_span_html_tags, Boolean, false, <<EOF)
Remove span HTML tags

If this option is `true`, the RemoveHtmlTags converter removes
span HTML tags.

Default: false
Used by: RemoveHtmlTags converter
EOF

    define(:header_offset, Integer, 0, <<EOF)
Sets the output offset for headers

If this option is c (may also be negative) then a header with level n
will be output as a header with level c+n. If c+n is lower than 1,
level 1 will be used. If c+n is greater than 6, level 6 will be used.

Default: 0
Used by: HTML converter, Kramdown converter, Latex converter
EOF

    define(:hard_wrap, Boolean, true, <<EOF)
Interprets line breaks literally

Insert HTML `<br />` tags inside paragraphs where the original Markdown
document had newlines (by default, Markdown ignores these newlines).

Default: true
Used by: GFM parser
EOF

    define(:syntax_highlighter, Symbol, :coderay, <<EOF)
Set the syntax highlighter

Specifies the syntax highlighter that should be used for highlighting
code blocks and spans. If this option is set to +nil+, no syntax
highlighting is done.

Options for the syntax highlighter can be set with the
syntax_highlighter_opts configuration option.

Default: coderay
Used by: HTML/Latex converter
EOF

    define(:syntax_highlighter_opts, Object, {}, <<EOF) do |val|
Set the syntax highlighter options

Specifies options for the syntax highlighter set via the
syntax_highlighter configuration option.

The value needs to be a hash with key-value pairs that are understood by
the used syntax highlighter.

Default: {}
Used by: HTML/Latex converter
EOF
      val = simple_hash_validator(val, :syntax_highlighter_opts)
      val.keys.each do |k|
        val[k.kind_of?(String) ? str_to_sym(k) : k] = val.delete(k)
      end
      val
    end

    define(:math_engine, Symbol, :mathjax, <<EOF)
Set the math engine

Specifies the math engine that should be used for converting math
blocks/spans. If this option is set to +nil+, no math engine is used and
the math blocks/spans are output as is.

Options for the selected math engine can be set with the
math_engine_opts configuration option.

Default: mathjax
Used by: HTML converter
EOF

    define(:math_engine_opts, Object, {}, <<EOF) do |val|
Set the math engine options

Specifies options for the math engine set via the math_engine
configuration option.

The value needs to be a hash with key-value pairs that are understood by
the used math engine.

Default: {}
Used by: HTML converter
EOF
      val = simple_hash_validator(val, :math_engine_opts)
      val.keys.each do |k|
        val[k.kind_of?(String) ? str_to_sym(k) : k] = val.delete(k)
      end
      val
    end

    define(:footnote_backlink, String, '&#8617;', <<EOF)
Defines the text that should be used for the footnote backlinks

The footnote backlink is just text, so any special HTML characters will
be escaped.

If the footnote backlint text is an empty string, no footnote backlinks
will be generated.

Default: '&8617;'
Used by: HTML converter
EOF

    define(:gfm_quirks, Object, [:paragraph_end], <<EOF) do |val|
Enables a set of GFM specific quirks

The way how GFM is transformed on Github often differs from the way
kramdown does things. Many of these differences are negligible but
others are not.

This option allows one to enable/disable certain GFM quirks, i.e. ways
in which GFM parsing differs from kramdown parsing.

The value has to be a list of quirk names that should be enabled,
separated by commas. Possible names are:

* paragraph_end

  Disables the kramdown restriction that at least one blank line has to
  be used after a paragraph before a new block element can be started.

  Note that if this quirk is used, lazy line wrapping does not fully
  work anymore!

Default: paragraph_end
Used by: GFM parser
EOF
      val = simple_array_validator(val, :gfm_quirks)
      val.map! {|v| str_to_sym(v.to_s)}
      val
    end

  end

end
