require File.join(File.dirname(__FILE__), 'test_helper')

class NumerizerTest < Test::Unit::TestCase
	def test_straight_parsing
		strings = { 1 => 'one',
			5 => 'five',
			10 => 'ten',
			11 => 'eleven',
			12 => 'twelve',
			13 => 'thirteen',
			14 => 'fourteen',
			15 => 'fifteen',
			16 => 'sixteen',
			17 => 'seventeen',
			18 => 'eighteen',
			19 => 'nineteen',
			20 => 'twenty',
			27 => 'twenty seven',
			31 => 'thirty-one',
			41 => 'forty one',
			42 => 'fourty two',
			59 => 'fifty nine',
			100 => 'a hundred',
			100 => 'one hundred',
			150 => 'one hundred and fifty',
			# 150 => 'one fifty',
			200 => 'two-hundred',
			500 => '5 hundred',
			999 => 'nine hundred and ninety nine',
			1_000 => 'one thousand',
			1_200 => 'twelve hundred',
			1_200 => 'one thousand two hundred',
			17_000 => 'seventeen thousand',
      21_473 => 'twentyone-thousand-four-hundred-and-seventy-three',
			74_002 => 'seventy four thousand and two',
			99_999 => 'ninety nine thousand nine hundred ninety nine',
			100_000 => '100 thousand',
			250_000 => 'two hundred fifty thousand',
			1_000_000 => 'one million',
			1_250_007 => 'one million two hundred fifty thousand and seven',
			1_000_000_000 => 'one billion',
			1_000_000_001 => 'one billion and one' }

		strings.keys.sort.each do |key|
			assert_equal key, Numerizer.numerize(strings[key]).to_i
		end
		
		assert_equal "2.5", Numerizer.numerize("two and a half")
		assert_equal "1/2", Numerizer.numerize("one half")
	end
	
	def test_combined_double_digets
	  assert_equal "21", Numerizer.numerize("twentyone")
	  assert_equal "37", Numerizer.numerize("thirtyseven")
  end
  
  def test_fractions_in_words
    assert_equal "1/4", Numerizer.numerize("1 quarter")
    assert_equal "1/4", Numerizer.numerize("one quarter")
    assert_equal "1/4", Numerizer.numerize("a quarter")
    assert_equal "1/8", Numerizer.numerize("one eighth")
    
    assert_equal "3/4", Numerizer.numerize("three quarters")
    assert_equal "2/4", Numerizer.numerize("two fourths")
    assert_equal "3/8", Numerizer.numerize("three eighths")
  end
  
  def test_fractional_addition
    assert_equal "1.25", Numerizer.numerize("one and a quarter")
    assert_equal "2.375", Numerizer.numerize("two and three eighths")
    assert_equal "3.5 hours", Numerizer.numerize("three and a half hours")
  end
  
  def test_word_with_a_number
    assert_equal "pennyweight", Numerizer.numerize("pennyweight")
  end

	def test_edges
		assert_equal "27 Oct 2006 7:30am", Numerizer.numerize("27 Oct 2006 7:30am")
	end
	
	def test_multiple_slashes_should_not_be_evaluated
	  assert_equal '11/02/2007', Numerizer.numerize('11/02/2007')
  end
  
  def test_compatability
    assert_equal '1/2', Numerizer.numerize('1/2')
    assert_equal '05/06', Numerizer.numerize('05/06')
    assert_equal "3.5 hours", Numerizer.numerize("three and a half hours")
  end

end
