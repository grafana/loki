package termui

const (
	DOT      = '•'
	ELLIPSES = '…'

	UP_ARROW   = '▲'
	DOWN_ARROW = '▼'

	COLLAPSED = '+'
	EXPANDED  = '−'
)

var (
	BARS = [...]rune{' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

	SHADED_BLOCKS = [...]rune{' ', '░', '▒', '▓', '█'}

	IRREGULAR_BLOCKS = [...]rune{
		' ', '▘', '▝', '▀', '▖', '▌', '▞', '▛',
		'▗', '▚', '▐', '▜', '▄', '▙', '▟', '█',
	}

	BRAILLE_OFFSET = '\u2800'
	BRAILLE        = [4][2]rune{
		{'\u0001', '\u0008'},
		{'\u0002', '\u0010'},
		{'\u0004', '\u0020'},
		{'\u0040', '\u0080'},
	}

	DOUBLE_BRAILLE = map[[2]int]rune{
		[2]int{0, 0}: '⣀',
		[2]int{0, 1}: '⡠',
		[2]int{0, 2}: '⡐',
		[2]int{0, 3}: '⡈',

		[2]int{1, 0}: '⢄',
		[2]int{1, 1}: '⠤',
		[2]int{1, 2}: '⠔',
		[2]int{1, 3}: '⠌',

		[2]int{2, 0}: '⢂',
		[2]int{2, 1}: '⠢',
		[2]int{2, 2}: '⠒',
		[2]int{2, 3}: '⠊',

		[2]int{3, 0}: '⢁',
		[2]int{3, 1}: '⠡',
		[2]int{3, 2}: '⠑',
		[2]int{3, 3}: '⠉',
	}

	SINGLE_BRAILLE_LEFT  = [4]rune{'\u2840', '⠄', '⠂', '⠁'}
	SINGLE_BRAILLE_RIGHT = [4]rune{'\u2880', '⠠', '⠐', '⠈'}
)
