# -*- coding: utf-8 -*-
#
#--
# Copyright (C) 2009-2016 Thomas Leitner <t_leitner@gmx.at>
#
# This file is part of kramdown which is licensed under the MIT.
#++
#

require 'set'
require 'kramdown/converter'

module Kramdown

  module Converter

    # Converts an element tree to LaTeX.
    #
    # This converter uses ideas from other Markdown-to-LaTeX converters like Pandoc and Maruku.
    #
    # You can customize this converter by sub-classing it and overriding the +convert_NAME+ methods.
    # Each such method takes the following parameters:
    #
    # [+el+] The element of type +NAME+ to be converted.
    #
    # [+opts+] A hash containing processing options that are passed down from parent elements. The
    #          key :parent is always set and contains the parent element as value.
    #
    # The return value of such a method has to be a string containing the element +el+ formatted
    # correctly as LaTeX markup.
    class Latex < Base

      # Initialize the LaTeX converter with the +root+ element and the conversion +options+.
      def initialize(root, options)
        super
        @data[:packages] = Set.new
      end

      # Dispatch the conversion of the element +el+ to a +convert_TYPE+ method using the +type+ of
      # the element.
      def convert(el, opts = {})
        send("convert_#{el.type}", el, opts)
      end

      # Return the converted content of the children of +el+ as a string.
      def inner(el, opts)
        result = ''
        options = opts.dup.merge(:parent => el)
        el.children.each_with_index do |inner_el, index|
          options[:index] = index
          options[:result] = result
          result << send("convert_#{inner_el.type}", inner_el, options)
        end
        result
      end

      def convert_root(el, opts)
        inner(el, opts)
      end

      def convert_blank(el, opts)
        opts[:result] =~ /\n\n\Z|\A\Z/ ? "" : "\n"
      end

      def convert_text(el, opts)
        escape(el.value)
      end

      def convert_p(el, opts)
        if el.children.size == 1 && el.children.first.type == :img && !(img = convert_img(el.children.first, opts)).empty?
          convert_standalone_image(el, opts, img)
        else
          "#{latex_link_target(el)}#{inner(el, opts)}\n\n"
        end
      end

      # Helper method used by +convert_p+ to convert a paragraph that only contains a single :img
      # element.
      def convert_standalone_image(el, opts, img)
        attrs = attribute_list(el)
        "\\begin{figure}#{attrs}\n\\begin{center}\n#{img}\n\\end{center}\n\\caption{#{escape(el.children.first.attr['alt'])}}\n#{latex_link_target(el, true)}\n\\end{figure}#{attrs}\n"
      end

      def convert_codeblock(el, opts)
        show_whitespace = el.attr['class'].to_s =~ /\bshow-whitespaces\b/
        lang = extract_code_language(el.attr)

        if @options[:syntax_highlighter] == :minted &&
            (highlighted_code = highlight_code(el.value, lang, :block))
          @data[:packages] << 'minted'
          "#{latex_link_target(el)}#{highlighted_code}\n"
        elsif show_whitespace || lang
          options = []
          options << "showspaces=%s,showtabs=%s" % (show_whitespace ? ['true', 'true'] : ['false', 'false'])
          options << "language=#{lang}" if lang
          options << "basicstyle=\\ttfamily\\footnotesize,columns=fixed,frame=tlbr"
          id = el.attr['id']
          options << "label=#{id}" if id
          attrs = attribute_list(el)
          "#{latex_link_target(el)}\\begin{lstlisting}[#{options.join(',')}]\n#{el.value}\n\\end{lstlisting}#{attrs}\n"
        else
          "#{latex_link_target(el)}\\begin{verbatim}#{el.value}\\end{verbatim}\n"
        end
      end

      def convert_blockquote(el, opts)
        latex_environment(el.children.size > 1 ? 'quotation' : 'quote', el, inner(el, opts))
      end

      def convert_header(el, opts)
        type = @options[:latex_headers][output_header_level(el.options[:level]) - 1]
        if ((id = el.attr['id']) ||
            (@options[:auto_ids] && (id = generate_id(el.options[:raw_text])))) && in_toc?(el)
          "\\#{type}{#{inner(el, opts)}}\\hypertarget{#{id}}{}\\label{#{id}}\n\n"
        else
          "\\#{type}*{#{inner(el, opts)}}\n\n"
        end
      end

      def convert_hr(el, opts)
        attrs = attribute_list(el)
        "#{latex_link_target(el)}\\begin{center}#{attrs}\n\\rule{3in}{0.4pt}\n\\end{center}#{attrs}\n"
      end

      def convert_ul(el, opts)
        if !@data[:has_toc] && (el.options[:ial][:refs].include?('toc') rescue nil)
          @data[:has_toc] = true
          '\tableofcontents'
        else
          latex_environment(el.type == :ul ? 'itemize' : 'enumerate', el, inner(el, opts))
        end
      end
      alias :convert_ol :convert_ul

      def convert_dl(el, opts)
        latex_environment('description', el, inner(el, opts))
      end

      def convert_li(el, opts)
        "\\item #{latex_link_target(el, true)}#{inner(el, opts).sub(/\n+\Z/, '')}\n"
      end

      def convert_dt(el, opts)
        "\\item[#{inner(el, opts)}] "
      end

      def convert_dd(el, opts)
        "#{latex_link_target(el)}#{inner(el, opts)}\n\n"
      end

      def convert_html_element(el, opts)
        if el.value == 'i' || el.value == 'em'
          "\\emph{#{inner(el, opts)}}"
        elsif el.value == 'b' || el.value == 'strong'
          "\\textbf{#{inner(el, opts)}}"
        else
          warning("Can't convert HTML element")
          ''
        end
      end

      def convert_xml_comment(el, opts)
        el.value.split(/\n/).map {|l| "% #{l}"}.join("\n") + "\n"
      end

      def convert_xml_pi(el, opts)
        warning("Can't convert XML PI")
        ''
      end

      TABLE_ALIGNMENT_CHAR = {:default => 'l', :left => 'l', :center => 'c', :right => 'r'} # :nodoc:

      def convert_table(el, opts)
        @data[:packages] << 'longtable'
        align = el.options[:alignment].map {|a| TABLE_ALIGNMENT_CHAR[a]}.join('|')
        attrs = attribute_list(el)
        "#{latex_link_target(el)}\\begin{longtable}{|#{align}|}#{attrs}\n\\hline\n#{inner(el, opts)}\\hline\n\\end{longtable}#{attrs}\n\n"
      end

      def convert_thead(el, opts)
        "#{inner(el, opts)}\\hline\n"
      end

      def convert_tbody(el, opts)
        inner(el, opts)
      end

      def convert_tfoot(el, opts)
        "\\hline \\hline \n#{inner(el, opts)}"
      end

      def convert_tr(el, opts)
        el.children.map {|c| send("convert_#{c.type}", c, opts)}.join(' & ') << "\\\\\n"
      end

      def convert_td(el, opts)
        inner(el, opts)
      end

      def convert_comment(el, opts)
        el.value.split(/\n/).map {|l| "% #{l}"}.join("\n") << "\n"
      end

      def convert_br(el, opts)
        res = "\\newline"
        res << "\n" if (c = opts[:parent].children[opts[:index]+1]) && (c.type != :text || c.value !~ /^\s*\n/)
        res
      end

      def convert_a(el, opts)
        url = el.attr['href']
        if url.start_with?('#')
          "\\hyperlink{#{escape(url[1..-1])}}{#{inner(el, opts)}}"
        else
          "\\href{#{escape(url)}}{#{inner(el, opts)}}"
        end
      end

      def convert_img(el, opts)
        line = el.options[:location]
        if el.attr['src'] =~ /^(https?|ftps?):\/\//
          warning("Cannot include non-local image#{line ? " (line #{line})" : ''}")
          ''
        elsif !el.attr['src'].empty?
          @data[:packages] << 'graphicx'
          "#{latex_link_target(el)}\\includegraphics{#{el.attr['src']}}"
        else
          warning("Cannot include image with empty path#{line ? " (line #{line})" : ''}")
          ''
        end
      end

      def convert_codespan(el, opts)
        lang = extract_code_language(el.attr)
        if @options[:syntax_highlighter] == :minted &&
            (highlighted_code = highlight_code(el.value, lang, :span))
          @data[:packages] << 'minted'
          "#{latex_link_target(el)}#{highlighted_code}"
        else
          "\\texttt{#{latex_link_target(el)}#{escape(el.value)}}"
        end
      end

      def convert_footnote(el, opts)
        @data[:packages] << 'fancyvrb'
        "\\footnote{#{inner(el.value, opts).rstrip}}"
      end

      def convert_raw(el, opts)
        if !el.options[:type] || el.options[:type].empty? || el.options[:type].include?('latex')
          el.value + (el.options[:category] == :block ? "\n" : '')
        else
          ''
        end
      end

      def convert_em(el, opts)
        "\\emph{#{latex_link_target(el)}#{inner(el, opts)}}"
      end

      def convert_strong(el, opts)
        "\\textbf{#{latex_link_target(el)}#{inner(el, opts)}}"
      end

      # Inspired by Maruku: entity conversion table based on the one from htmltolatex
      # (http://sourceforge.net/projects/htmltolatex/), with some small adjustments/additions
      ENTITY_CONV_TABLE = {
        913 => ['$A$'],
        914 => ['$B$'],
        915 => ['$\Gamma$'],
        916 => ['$\Delta$'],
        917 => ['$E$'],
        918 => ['$Z$'],
        919 => ['$H$'],
        920 => ['$\Theta$'],
        921 => ['$I$'],
        922 => ['$K$'],
        923 => ['$\Lambda$'],
        924 => ['$M$'],
        925 => ['$N$'],
        926 => ['$\Xi$'],
        927 => ['$O$'],
        928 => ['$\Pi$'],
        929 => ['$P$'],
        931 => ['$\Sigma$'],
        932 => ['$T$'],
        933 => ['$Y$'],
        934 => ['$\Phi$'],
        935 => ['$X$'],
        936 => ['$\Psi$'],
        937 => ['$\Omega$'],
        945 => ['$\alpha$'],
        946 => ['$\beta$'],
        947 => ['$\gamma$'],
        948 => ['$\delta$'],
        949 => ['$\epsilon$'],
        950 => ['$\zeta$'],
        951 => ['$\eta$'],
        952 => ['$\theta$'],
        953 => ['$\iota$'],
        954 => ['$\kappa$'],
        955 => ['$\lambda$'],
        956 => ['$\mu$'],
        957 => ['$\nu$'],
        958 => ['$\xi$'],
        959 => ['$o$'],
        960 => ['$\pi$'],
        961 => ['$\rho$'],
        963 => ['$\sigma$'],
        964 => ['$\tau$'],
        965 => ['$\upsilon$'],
        966 => ['$\phi$'],
        967 => ['$\chi$'],
        968 => ['$\psi$'],
        969 => ['$\omega$'],
        962 => ['$\varsigma$'],
        977 => ['$\vartheta$'],
        982 => ['$\varpi$'],
        8230 => ['\ldots'],
        8242 => ['$\prime$'],
        8254 => ['-'],
        8260 => ['/'],
        8472 => ['$\wp$'],
        8465 => ['$\Im$'],
        8476 => ['$\Re$'],
        8501 => ['$\aleph$'],
        8226 => ['$\bullet$'],
        8482 => ['$^{\rm TM}$'],
        8592 => ['$\leftarrow$'],
        8594 => ['$\rightarrow$'],
        8593 => ['$\uparrow$'],
        8595 => ['$\downarrow$'],
        8596 => ['$\leftrightarrow$'],
        8629 => ['$\hookleftarrow$'],
        8657 => ['$\Uparrow$'],
        8659 => ['$\Downarrow$'],
        8656 => ['$\Leftarrow$'],
        8658 => ['$\Rightarrow$'],
        8660 => ['$\Leftrightarrow$'],
        8704 => ['$\forall$'],
        8706 => ['$\partial$'],
        8707 => ['$\exists$'],
        8709 => ['$\emptyset$'],
        8711 => ['$\nabla$'],
        8712 => ['$\in$'],
        8715 => ['$\ni$'],
        8713 => ['$\notin$'],
        8721 => ['$\sum$'],
        8719 => ['$\prod$'],
        8722 => ['$-$'],
        8727 => ['$\ast$'],
        8730 => ['$\surd$'],
        8733 => ['$\propto$'],
        8734 => ['$\infty$'],
        8736 => ['$\angle$'],
        8743 => ['$\wedge$'],
        8744 => ['$\vee$'],
        8745 => ['$\cup$'],
        8746 => ['$\cap$'],
        8747 => ['$\int$'],
        8756 => ['$\therefore$', 'amssymb'],
        8764 => ['$\sim$'],
        8776 => ['$\approx$'],
        8773 => ['$\cong$'],
        8800 => ['$\neq$'],
        8801 => ['$\equiv$'],
        8804 => ['$\leq$'],
        8805 => ['$\geq$'],
        8834 => ['$\subset$'],
        8835 => ['$\supset$'],
        8838 => ['$\subseteq$'],
        8839 => ['$\supseteq$'],
        8836 => ['$\nsubset$', 'amssymb'],
        8853 => ['$\oplus$'],
        8855 => ['$\otimes$'],
        8869 => ['$\perp$'],
        8901 => ['$\cdot$'],
        8968 => ['$\rceil$'],
        8969 => ['$\lceil$'],
        8970 => ['$\lfloor$'],
        8971 => ['$\rfloor$'],
        9001 => ['$\rangle$'],
        9002 => ['$\langle$'],
        9674 => ['$\lozenge$', 'amssymb'],
        9824 => ['$\spadesuit$'],
        9827 => ['$\clubsuit$'],
        9829 => ['$\heartsuit$'],
        9830 => ['$\diamondsuit$'],
        38 => ['\&'],
        34 => ['"'],
        39 => ['\''],
        169 => ['\copyright'],
        60 => ['\textless'],
        62 => ['\textgreater'],
        338 => ['\OE'],
        339 => ['\oe'],
        352 => ['\v{S}'],
        353 => ['\v{s}'],
        376 => ['\"Y'],
        710 => ['\textasciicircum'],
        732 => ['\textasciitilde'],
        8211 => ['--'],
        8212 => ['---'],
        8216 => ['`'],
        8217 => ['\''],
        8220 => ['``'],
        8221 => ['\'\''],
        8224 => ['\dag'],
        8225 => ['\ddag'],
        8240 => ['\permil', 'wasysym'],
        8364 => ['\euro', 'eurosym'],
        8249 => ['\guilsinglleft'],
        8250 => ['\guilsinglright'],
        8218 => ['\quotesinglbase', 'mathcomp'],
        8222 => ['\quotedblbase', 'mathcomp'],
        402 => ['\textflorin', 'mathcomp'],
        381 => ['\v{Z}'],
        382 => ['\v{z}'],
        160 => ['~'],
        161 => ['\textexclamdown'],
        163 => ['\pounds'],
        164 => ['\currency', 'wasysym'],
        165 => ['\textyen', 'textcomp'],
        166 => ['\brokenvert', 'wasysym'],
        167 => ['\S'],
        171 => ['\guillemotleft'],
        187 => ['\guillemotright'],
        174 => ['\textregistered'],
        170 => ['\textordfeminine'],
        172 => ['$\neg$'],
        173 => ['\-'],
        176 => ['$\degree$', 'mathabx'],
        177 => ['$\pm$'],
        180 => ['\''],
        181 => ['$\mu$'],
        182 => ['\P'],
        183 => ['$\cdot$'],
        186 => ['\textordmasculine'],
        162 => ['\cent', 'wasysym'],
        185 => ['$^1$'],
        178 => ['$^2$'],
        179 => ['$^3$'],
        189 => ['$\frac{1}{2}$'],
        188 => ['$\frac{1}{4}$'],
        190 => ['$\frac{3}{4}'],
        192 => ['\`A'],
        193 => ['\\\'A'],
        194 => ['\^A'],
        195 => ['\~A'],
        196 => ['\"A'],
        197 => ['\AA'],
        198 => ['\AE'],
        199 => ['\cC'],
        200 => ['\`E'],
        201 => ['\\\'E'],
        202 => ['\^E'],
        203 => ['\"E'],
        204 => ['\`I'],
        205 => ['\\\'I'],
        206 => ['\^I'],
        207 => ['\"I'],
        208 => ['$\eth$', 'amssymb'],
        209 => ['\~N'],
        210 => ['\`O'],
        211 => ['\\\'O'],
        212 => ['\^O'],
        213 => ['\~O'],
        214 => ['\"O'],
        215 => ['$\times$'],
        216 => ['\O'],
        217 => ['\`U'],
        218 => ['\\\'U'],
        219 => ['\^U'],
        220 => ['\"U'],
        221 => ['\\\'Y'],
        222 => ['\Thorn', 'wasysym'],
        223 => ['\ss'],
        224 => ['\`a'],
        225 => ['\\\'a'],
        226 => ['\^a'],
        227 => ['\~a'],
        228 => ['\"a'],
        229 => ['\aa'],
        230 => ['\ae'],
        231 => ['\cc'],
        232 => ['\`e'],
        233 => ['\\\'e'],
        234 => ['\^e'],
        235 => ['\"e'],
        236 => ['\`i'],
        237 => ['\\\'i'],
        238 => ['\^i'],
        239 => ['\"i'],
        240 => ['$\eth$'],
        241 => ['\~n'],
        242 => ['\`o'],
        243 => ['\\\'o'],
        244 => ['\^o'],
        245 => ['\~o'],
        246 => ['\"o'],
        247 => ['$\divide$'],
        248 => ['\o'],
        249 => ['\`u'],
        250 => ['\\\'u'],
        251 => ['\^u'],
        252 => ['\"u'],
        253 => ['\\\'y'],
        254 => ['\thorn', 'wasysym'],
        255 => ['\"y'],
        8201 => ['\thinspace'],
        8194 => ['\hskip .5em\relax'],
        8195 => ['\quad'],
      } # :nodoc:
      ENTITY_CONV_TABLE.each {|k,v| ENTITY_CONV_TABLE[k][0].insert(-1, '{}')}

      def entity_to_latex(entity)
        text, package = ENTITY_CONV_TABLE[entity.code_point]
        if text
          @data[:packages] << package if package
          text
        else
          warning("Couldn't find entity with code #{entity.code_point} in substitution table!")
          ''
        end
      end

      def convert_entity(el, opts)
        entity_to_latex(el.value)
      end

      TYPOGRAPHIC_SYMS = {
        :mdash => '---', :ndash => '--', :hellip => '\ldots{}',
        :laquo_space => '\guillemotleft{}~', :raquo_space => '~\guillemotright{}',
        :laquo => '\guillemotleft{}', :raquo => '\guillemotright{}'
      } # :nodoc:
      def convert_typographic_sym(el, opts)
        TYPOGRAPHIC_SYMS[el.value]
      end

      def convert_smart_quote(el, opts)
        res = entity_to_latex(smart_quote_entity(el)).chomp('{}')
        res << "{}" if ((nel = opts[:parent].children[opts[:index]+1]) && nel.type == :smart_quote) || res =~ /\w$/
        res
      end

      def convert_math(el, opts)
        @data[:packages] += %w[amssymb amsmath amsthm amsfonts]
        if el.options[:category] == :block
          if el.value =~ /\A\s*\\begin\{/
            el.value
          else
            latex_environment('displaymath', el, el.value)
          end
        else
          "$#{el.value}$"
        end
      end

      def convert_abbreviation(el, opts)
        @data[:packages] += %w[acronym]
        "\\ac{#{normalize_abbreviation_key(el.value)}}"
      end

      # Normalize the abbreviation key so that it only contains allowed ASCII character
      def normalize_abbreviation_key(key)
        key.gsub(/\W/) {|m| m.unpack('H*').first}
      end

      # Wrap the +text+ inside a LaTeX environment of type +type+. The element +el+ is passed on to
      # the method #attribute_list -- the resulting string is appended to both the \\begin and the
      # \\end lines of the LaTeX environment for easier post-processing of LaTeX environments.
      def latex_environment(type, el, text)
        attrs = attribute_list(el)
        "\\begin{#{type}}#{latex_link_target(el)}#{attrs}\n#{text.rstrip}\n\\end{#{type}}#{attrs}\n"
      end

      # Return a string containing a valid \hypertarget command if the element has an ID defined, or
      # +nil+ otherwise. If the parameter +add_label+ is +true+, a \label command will also be used
      # additionally to the \hypertarget command.
      def latex_link_target(el, add_label = false)
        if (id = el.attr['id'])
          "\\hypertarget{#{id}}{}" << (add_label ? "\\label{#{id}}" : '')
        else
          nil
        end
      end

      # Return a LaTeX comment containing all attributes as 'key="value"' pairs.
      def attribute_list(el)
        attrs = el.attr.map {|k,v| v.nil? ? '' : " #{k}=\"#{v.to_s}\""}.compact.sort.join('')
        attrs = "   % #{attrs}" if !attrs.empty?
        attrs
      end

      ESCAPE_MAP = {
        "^"  => "\\^{}",
        "\\" => "\\textbackslash{}",
        "~"  => "\\ensuremath{\\sim}",
        "|"  => "\\textbar{}",
        "<"  => "\\textless{}",
        ">"  => "\\textgreater{}"
      }.merge(Hash[*("{}$%&_#".scan(/./).map {|c| [c, "\\#{c}"]}.flatten)]) # :nodoc:
      ESCAPE_RE = Regexp.union(*ESCAPE_MAP.collect {|k,v| k}) # :nodoc:

      # Escape the special LaTeX characters in the string +str+.
      def escape(str)
        str.gsub(ESCAPE_RE) {|m| ESCAPE_MAP[m]}
      end

    end

  end
end
