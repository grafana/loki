# -*- coding: utf-8 -*-
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#

module Kramdown

  # Represents all elements in the element tree.
  #
  # kramdown only uses this one class for representing all available elements in an element tree
  # (paragraphs, headers, emphasis, ...). The type of element can be set via the #type accessor.
  #
  # Following is a description of all supported element types.
  #
  # Note that the option :location may contain the start line number of an element in the source
  # document.
  #
  # == Structural Elements
  #
  # === :root
  #
  # [Category] None
  # [Usage context] As the root element of a document
  # [Content model] Block-level elements
  #
  # Represents the root of a kramdown document.
  #
  # The root element contains the following option keys:
  #
  # :encoding:: When running on Ruby 1.9 this key has to be set to the encoding used for the text
  #             parts of the kramdown document.
  #
  # :abbrev_defs:: This key may be used to store the mapping of abbreviation to abbreviation
  #                definition.
  #
  # :abbrev_attr:: This key may be used to store the mapping of abbreviation to abbreviation
  #                attributes.
  #
  # :options:: This key may be used to store options that were set during parsing of the document.
  #
  #
  # === :blank
  #
  # [Category] Block-level element
  # [Usage context] Where block-level elements are expected
  # [Content model] Empty
  #
  # Represents one or more blank lines. It is not allowed to have two or more consecutive blank
  # elements.
  #
  # The +value+ field may contain the original content of the blank lines.
  #
  #
  # === :p
  #
  # [Category] Block-level element
  # [Usage context] Where block-level elements are expected
  # [Content model] Span-level elements
  #
  # Represents a paragraph.
  #
  # If the option :transparent is +true+, this element just represents a block of text. I.e. this
  # element just functions as a container for span-level elements.
  #
  #
  # === :header
  #
  # [Category] Block-level element
  # [Usage context] Where block-level elements are expected
  # [Content model] Span-level elements
  #
  # Represents a header.
  #
  # The option :level specifies the header level and has to contain a number between 1 and \6. The
  # option :raw_text has to contain the raw header text.
  #
  #
  # === :blockquote
  #
  # [Category] Block-level element
  # [Usage context] Where block-level elements are expected
  # [Content model] Block-level elements
  #
  # Represents a blockquote.
  #
  #
  # === :codeblock
  #
  # [Category] Block-level element
  # [Usage context] Where block-level elements are expected
  # [Content model] Empty
  #
  # Represents a code block, i.e. a block of text that should be used as-is.
  #
  # The +value+ field has to contain the content of the code block.
  #
  # The option :lang specifies a highlighting language with possible HTML style options (e.g.
  # php?start_inline=1) and should be used instead of a possibly also available language embedded in
  # a class name of the form 'language-LANG'.
  #
  #
  # === :ul
  #
  # [Category] Block-level element
  # [Usage context] Where block-level elements are expected
  # [Content model] One or more :li elements
  #
  # Represents an unordered list.
  #
  #
  # === :ol
  #
  # [Category] Block-level element
  # [Usage context] Where block-level elements are expected
  # [Content model] One or more :li elements
  #
  # Represents an ordered list.
  #
  #
  # === :li
  #
  # [Category] Block-level element
  # [Usage context] Inside :ol and :ul elements
  # [Content model] Block-level elements
  #
  # Represents a list item of an ordered or unordered list.
  #
  # Note that the first child of a list item must not be a :blank element!
  #
  #
  # === :dl
  #
  # [Category] Block-level element
  # [Usage context] Where block-level elements are expected
  # [Content model] One or more groups each consisting of one or more :dt elements followed by one
  #                 or more :dd elements.
  #
  # Represents a definition list which contains groups consisting of terms and definitions for them.
  #
  #
  # === :dt
  #
  # [Category] Block-level element
  # [Usage context] Before :dt or :dd elements inside a :dl elment
  # [Content model] Span-level elements
  #
  # Represents the term part of a term-definition group in a definition list.
  #
  #
  # === :dd
  #
  # [Category] Block-level element
  # [Usage context] After :dt or :dd elements inside a :dl elment
  # [Content model] Block-level elements
  #
  # Represents the definition part of a term-definition group in a definition list.
  #
  #
  # === :hr
  #
  # [Category] Block-level element
  # [Usage context] Where block-level elements are expected
  # [Content model] None
  #
  # Represents a horizontal line.
  #
  #
  # === :table
  #
  # [Category] Block-level element
  # [Usage context] Where block-level elements are expected
  # [Content model] Zero or one :thead elements, one or more :tbody elements, zero or one :tfoot
  #                 elements
  #
  # Represents a table. Each table row (i.e. :tr element) of the table has to contain the same
  # number of :td elements.
  #
  # The option :alignment has to be an array containing the alignment values, exactly one for each
  # column of the table. The possible alignment values are :left, :center, :right and :default.
  #
  #
  # === :thead
  #
  # [Category] None
  # [Usage context] As first element inside a :table element
  # [Content model] One or more :tr elements
  #
  # Represents the table header.
  #
  #
  # === :tbody
  #
  # [Category] None
  # [Usage context] After a :thead element but before a :tfoot element inside a :table element
  # [Content model] One or more :tr elements
  #
  # Represents a table body.
  #
  #
  # === :tfoot
  #
  # [Category] None
  # [Usage context] As last element inside a :table element
  # [Content model] One or more :tr elements
  #
  # Represents the table footer.
  #
  #
  # === :tr
  #
  # [Category] None
  # [Usage context] Inside :thead, :tbody and :tfoot elements
  # [Content model] One or more :td elements
  #
  # Represents a table row.
  #
  #
  # === :td
  #
  # [Category] Block-level element
  # [Usage context] Inside :tr elements
  # [Content model] As child of :thead/:tr span-level elements, as child of :tbody/:tr and
  #                 :tfoot/:tr block-level elements
  #
  # Represents a table cell.
  #
  #
  # === :math
  #
  # [Category] Block/span-level element
  # [Usage context] Where block/span-level elements are expected
  # [Content model] None
  #
  # Represents mathematical text that is written in LaTeX.
  #
  # The +value+ field has to contain the actual mathematical text.
  #
  # The option :category has to be set to either :span or :block depending on the context where the
  # element is used.
  #
  #
  # == Text Markup Elements
  #
  # === :text
  #
  # [Category] Span-level element
  # [Usage context] Where span-level elements are expected
  # [Content model] None
  #
  # Represents text.
  #
  # The +value+ field has to contain the text itself.
  #
  #
  # === :br
  #
  # [Category] Span-level element
  # [Usage context] Where span-level elements are expected
  # [Content model] None
  #
  # Represents a hard line break.
  #
  #
  # === :a
  #
  # [Category] Span-level element
  # [Usage context] Where span-level elements are expected
  # [Content model] Span-level elements
  #
  # Represents a link to an URL.
  #
  # The attribute +href+ has to be set to the URL to which the link points. The attribute +title+
  # optionally contains the title of the link.
  #
  #
  # === :img
  #
  # [Category] Span-level element
  # [Usage context] Where span-level elements are expected
  # [Content model] None
  #
  # Represents an image.
  #
  # The attribute +src+ has to be set to the URL of the image. The attribute +alt+ has to contain a
  # text description of the image. The attribute +title+ optionally contains the title of the image.
  #
  #
  # === :codespan
  #
  # [Category] Span-level element
  # [Usage context] Where span-level elements are expected
  # [Content model] None
  #
  # Represents verbatim text.
  #
  # The +value+ field has to contain the content of the code span.
  #
  #
  # === :footnote
  #
  # [Category] Span-level element
  # [Usage context] Where span-level elements are expected
  # [Content model] None
  #
  # Represents a footnote marker.
  #
  # The +value+ field has to contain an element whose children are the content of the footnote. The
  # option :name has to contain a valid and unique footnote name. A valid footnote name consists of
  # a word character or a digit and then optionally followed by other word characters, digits or
  # dashes.
  #
  #
  # === :em
  #
  # [Category] Span-level element
  # [Usage context] Where span-level elements are expected
  # [Content model] Span-level elements
  #
  # Represents emphasis of its contents.
  #
  #
  # === :strong
  #
  # [Category] Span-level element
  # [Usage context] Where span-level elements are expected
  # [Content model] Span-level elements
  #
  # Represents strong importance for its contents.
  #
  #
  # === :entity
  #
  # [Category] Span-level element
  # [Usage context] Where span-level elements are expected
  # [Content model] None
  #
  # Represents an HTML entity.
  #
  # The +value+ field has to contain an instance of Kramdown::Utils::Entities::Entity. The option
  # :original can be used to store the original representation of the entity.
  #
  #
  # === :typographic_sym
  #
  # [Category] Span-level element
  # [Usage context] Where span-level elements are expected
  # [Content model] None
  #
  # Represents a typographic symbol.
  #
  # The +value+ field needs to contain a Symbol representing the specific typographic symbol from
  # the following list:
  #
  # :mdash:: An mdash character (---)
  # :ndash:: An ndash character (--)
  # :hellip:: An ellipsis (...)
  # :laquo:: A left guillemet (<<)
  # :raquo:: A right guillemet (>>)
  # :laquo_space:: A left guillemet with a space (<< )
  # :raquo_space:: A right guillemet with a space ( >>)
  #
  #
  # === :smart_quote
  #
  # [Category] Span-level element
  # [Usage context] Where span-level elements are expected
  # [Content model] None
  #
  # Represents a quotation character.
  #
  # The +value+ field needs to contain a Symbol representing the specific quotation character:
  #
  # :lsquo:: Left single quote
  # :rsquo:: Right single quote
  # :ldquo:: Left double quote
  # :rdquo:: Right double quote
  #
  #
  # === :abbreviation
  #
  # [Category] Span-level element
  # [Usage context] Where span-level elements are expected
  # [Content model] None
  #
  # Represents a text part that is an abbreviation.
  #
  # The +value+ field has to contain the text part that is the abbreviation. The definition of the
  # abbreviation is stored in the :root element of the document.
  #
  #
  # == Other Elements
  #
  # === :html_element
  #
  # [Category] Block/span-level element
  # [Usage context] Where block/span-level elements or raw HTML elements are expected
  # [Content model] Depends on the element
  #
  # Represents an HTML element.
  #
  # The +value+ field has to contain the name of the HTML element the element is representing.
  #
  # The option :category has to be set to either :span or :block depending on the whether the
  # element is a block-level or a span-level element. The option :content_model has to be set to the
  # content model for the element (either :block if it contains block-level elements, :span if it
  # contains span-level elements or :raw if it contains raw content).
  #
  #
  # === :xml_comment
  #
  # [Category] Block/span-level element
  # [Usage context] Where block/span-level elements are expected or in raw HTML elements
  # [Content model] None
  #
  # Represents an XML/HTML comment.
  #
  # The +value+ field has to contain the whole XML/HTML comment including the delimiters.
  #
  # The option :category has to be set to either :span or :block depending on the context where the
  # element is used.
  #
  #
  # === :xml_pi
  #
  # [Category] Block/span-level element
  # [Usage context] Where block/span-level elements are expected or in raw HTML elements
  # [Content model] None
  #
  # Represents an XML/HTML processing instruction.
  #
  # The +value+ field has to contain the whole XML/HTML processing instruction including the
  # delimiters.
  #
  # The option :category has to be set to either :span or :block depending on the context where the
  # element is used.
  #
  #
  # === :comment
  #
  # [Category] Block/span-level element
  # [Usage context] Where block/span-level elements are expected
  # [Content model] None
  #
  # Represents a comment.
  #
  # The +value+ field has to contain the comment.
  #
  # The option :category has to be set to either :span or :block depending on the context where the
  # element is used. If it is set to :span, then no blank lines are allowed in the comment.
  #
  #
  # === :raw
  #
  # [Category] Block/span-level element
  # [Usage context] Where block/span-level elements are expected
  # [Content model] None
  #
  # Represents a raw string that should not be modified. For example, the element could contain some
  # HTML code that should be output as-is without modification and escaping.
  #
  # The +value+ field has to contain the actual raw text.
  #
  # The option :category has to be set to either :span or :block depending on the context where the
  # element is used. If it is set to :span, then no blank lines are allowed in the raw text.
  #
  # The option :type can be set to an array of strings to define for which converters the raw string
  # is valid.
  #
  class Element

    # A symbol representing the element type. For example, :p or :blockquote.
    attr_accessor :type

    # The value of the element. The interpretation of this field depends on the type of the element.
    # Many elements don't use this field.
    attr_accessor :value

    # The child elements of this element.
    attr_accessor :children


    # Create a new Element object of type +type+. The optional parameters +value+, +attr+ and
    # +options+ can also be set in this constructor for convenience.
    def initialize(type, value = nil, attr = nil, options = nil)
      @type, @value, @attr, @options = type, value, (Utils::OrderedHash.new.merge!(attr) if attr), options
      @children = []
    end

    # The attributes of the element. Uses an Utils::OrderedHash to retain the insertion order.
    def attr
      @attr ||= Utils::OrderedHash.new
    end

    # The options hash for the element. It is used for storing arbitray options.
    def options
      @options ||= {}
    end

    def inspect #:nodoc:
      "<kd:#{@type}#{@value.nil? ? '' : ' ' + @value.inspect} #{@attr.inspect}#{options.empty? ? '' : ' ' + @options.inspect}#{@children.empty? ? '' : ' ' + @children.inspect}>"
    end

    CATEGORY = {} # :nodoc:
    [:blank, :p, :header, :blockquote, :codeblock, :ul, :ol, :li, :dl, :dt, :dd, :table, :td, :hr].each {|b| CATEGORY[b] = :block}
    [:text, :a, :br, :img, :codespan, :footnote, :em, :strong, :entity, :typographic_sym,
     :smart_quote, :abbreviation].each {|b| CATEGORY[b] = :span}

    # Return the category of +el+ which can be :block, :span or +nil+.
    #
    # Most elements have a fixed category, however, some elements can either appear in a block-level
    # or a span-level context. These elements need to have the option :category correctly set.
    def self.category(el)
      CATEGORY[el.type] || el.options[:category]
    end

  end

end
