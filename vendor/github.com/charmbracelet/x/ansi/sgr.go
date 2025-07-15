package ansi

import "strconv"

// Select Graphic Rendition (SGR) is a command that sets display attributes.
//
// Default is 0.
//
//	CSI Ps ; Ps ... m
//
// See: https://vt100.net/docs/vt510-rm/SGR.html
func SelectGraphicRendition(ps ...Attr) string {
	if len(ps) == 0 {
		return ResetStyle
	}

	var s Style
	for _, p := range ps {
		attr, ok := attrStrings[p]
		if ok {
			s = append(s, attr)
		} else {
			if p < 0 {
				p = 0
			}
			s = append(s, strconv.Itoa(p))
		}
	}

	return s.String()
}

// SGR is an alias for [SelectGraphicRendition].
func SGR(ps ...Attr) string {
	return SelectGraphicRendition(ps...)
}

var attrStrings = map[int]string{
	ResetAttr:                        "0",
	BoldAttr:                         "1",
	FaintAttr:                        "2",
	ItalicAttr:                       "3",
	UnderlineAttr:                    "4",
	SlowBlinkAttr:                    "5",
	RapidBlinkAttr:                   "6",
	ReverseAttr:                      "7",
	ConcealAttr:                      "8",
	StrikethroughAttr:                "9",
	NoBoldAttr:                       "21",
	NormalIntensityAttr:              "22",
	NoItalicAttr:                     "23",
	NoUnderlineAttr:                  "24",
	NoBlinkAttr:                      "25",
	NoReverseAttr:                    "27",
	NoConcealAttr:                    "28",
	NoStrikethroughAttr:              "29",
	BlackForegroundColorAttr:         "30",
	RedForegroundColorAttr:           "31",
	GreenForegroundColorAttr:         "32",
	YellowForegroundColorAttr:        "33",
	BlueForegroundColorAttr:          "34",
	MagentaForegroundColorAttr:       "35",
	CyanForegroundColorAttr:          "36",
	WhiteForegroundColorAttr:         "37",
	ExtendedForegroundColorAttr:      "38",
	DefaultForegroundColorAttr:       "39",
	BlackBackgroundColorAttr:         "40",
	RedBackgroundColorAttr:           "41",
	GreenBackgroundColorAttr:         "42",
	YellowBackgroundColorAttr:        "43",
	BlueBackgroundColorAttr:          "44",
	MagentaBackgroundColorAttr:       "45",
	CyanBackgroundColorAttr:          "46",
	WhiteBackgroundColorAttr:         "47",
	ExtendedBackgroundColorAttr:      "48",
	DefaultBackgroundColorAttr:       "49",
	ExtendedUnderlineColorAttr:       "58",
	DefaultUnderlineColorAttr:        "59",
	BrightBlackForegroundColorAttr:   "90",
	BrightRedForegroundColorAttr:     "91",
	BrightGreenForegroundColorAttr:   "92",
	BrightYellowForegroundColorAttr:  "93",
	BrightBlueForegroundColorAttr:    "94",
	BrightMagentaForegroundColorAttr: "95",
	BrightCyanForegroundColorAttr:    "96",
	BrightWhiteForegroundColorAttr:   "97",
	BrightBlackBackgroundColorAttr:   "100",
	BrightRedBackgroundColorAttr:     "101",
	BrightGreenBackgroundColorAttr:   "102",
	BrightYellowBackgroundColorAttr:  "103",
	BrightBlueBackgroundColorAttr:    "104",
	BrightMagentaBackgroundColorAttr: "105",
	BrightCyanBackgroundColorAttr:    "106",
	BrightWhiteBackgroundColorAttr:   "107",
}
