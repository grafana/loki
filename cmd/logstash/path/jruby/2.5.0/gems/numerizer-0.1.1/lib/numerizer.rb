# LICENSE:
# 
# (The MIT License)
# 
# Copyright © 2008 Tom Preston-Werner
# 
# Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:
# 
# The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.
# 
# THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

require 'strscan'

class Numerizer

	DIRECT_NUMS = [
		['eleven', '11'],
		['twelve', '12'],
		['thirteen', '13'],
		['fourteen', '14'],
		['fifteen', '15'],
		['sixteen', '16'],
		['seventeen', '17'],
		['eighteen', '18'],
		['nineteen', '19'],
		['ninteen', '19'], # Common mis-spelling
		['zero', '0'],
		['ten', '10'],
		['\ba[\b^$]', '1'] # doesn't make sense for an 'a' at the end to be a 1
	]
	
	SINGLE_NUMS = [
	  ['one', 1],
  	['two', 2],
  	['three', 3],
  	#['four(\W|$)', '4\1'],  # The weird regex is so that it matches four but not fourty
  	['four', 4],
  	['five', 5],
  	['six', 6],
  	['seven', 7],
  	['eight', 8],
  	['nine', 9]
	]

	TEN_PREFIXES = [ ['twenty', 20],
		['thirty', 30],
		['forty', 40],
		['fourty', 40], # Common misspelling
		['fifty', 50],
		['sixty', 60],
		['seventy', 70],
		['eighty', 80],
		['ninety', 90]
	]

	BIG_PREFIXES = [ ['hundred', 100],
		['thousand', 1000],
		['million', 1_000_000],
		['billion', 1_000_000_000],
		['trillion', 1_000_000_000_000],
	]
	
	FRACTIONS = [ ['half', 2],
	  ['third(s)?', 3],
	  ['fourth(s)?', 4],
	  ['quarter(s)?', 4],
	  ['fifth(s)?', 5],
	  ['sixth(s)?', 6],
	  ['seventh(s)?', 7],
	  ['eighth(s)?', 8],
	  ['nineth(s)?', 9],
	]

	def self.numerize(string)
		string = string.dup

		# preprocess
		string.gsub!(/ +|([^\d])-([^\d])/, '\1 \2') # will mutilate hyphenated-words

		# easy/direct replacements

		(DIRECT_NUMS + SINGLE_NUMS).each do |dn|
      # string.gsub!(/#{dn[0]}/i, '<num>' + dn[1])
      string.gsub!(/(^|\W+)#{dn[0]}($|\W+)/i) {"#{$1}<num>" + dn[1].to_s + $2}
		end

		# ten, twenty, etc.
    # TEN_PREFIXES.each do |tp|
    #   string.gsub!(/(?:#{tp[0]}) *<num>(\d(?=[^\d]|$))*/i) {'<num>' + (tp[1] + $1.to_i).to_s}
    # end
		TEN_PREFIXES.each do |tp|
		  SINGLE_NUMS.each do |dn|
		    string.gsub!(/(^|\W+)#{tp[0]}#{dn[0]}($|\W+)/i) { 
		      "#{$1}<num>" + (tp[1] + dn[1]).to_s + $2
		    }
	    end
			string.gsub!(/(^|\W+)#{tp[0]}($|\W+)/i) { "#{$1}<num>" + tp[1].to_s + $2 }
		end
		
	  # handle fractions
		FRACTIONS.each do |tp|
		  string.gsub!(/a #{tp[0]}/i) { '<num>1/' + tp[1].to_s }
			string.gsub!(/\s#{tp[0]}/i) { '/' + tp[1].to_s }
		end
		
		# evaluate fractions when preceded by another number
    string.gsub!(/(\d+)(?: | and |-)+(<num>|\s)*(\d+)\s*\/\s*(\d+)/i) { ($1.to_f + ($3.to_f/$4.to_f)).to_s }
    
		# hundreds, thousands, millions, etc.
		BIG_PREFIXES.each do |bp|
			string.gsub!(/(?:<num>)?(\d*) *#{bp[0]}/i) { '<num>' + (bp[1] * $1.to_i).to_s}
        andition(string)
		end

    andition(string)

		string.gsub(/<num>/, '')
	end

	private

	def self.andition(string)
		sc = StringScanner.new(string)
		while(sc.scan_until(/<num>(\d+)( | and )<num>(\d+)(?=[^\w]|$)/i))
			if sc[2] =~ /and/ || sc[1].size > sc[3].size
				string[(sc.pos - sc.matched_size)..(sc.pos-1)] = '<num>' + (sc[1].to_i + sc[3].to_i).to_s
				sc.reset
			end
		end
	end

end
