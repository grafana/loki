package syntax

import (
	"bytes"
	"cmp"
	"slices"
)

type FindOptimizations struct {
	rightToLeft  bool
	asciiLookups [][]uint

	FindMode          FindNextStartingPositionMode
	LeadingAnchor     NodeType
	TrailingAnchor    NodeType
	MinRequiredLength int
	MaxPossibleLength int
	LeadingPrefix     string
	LeadingPrefixes   []string
	//LeadingStrings    *helpers.StringSearchValues

	FixedDistanceLiteral FixedDistanceLiteral
	FixedDistanceSets    []FixedDistanceSet
	LiteralAfterLoop     *LiteralAfterLoop
	LandmarkChain        *RequiredLandmarkChain
}

type LiteralAfterLoop struct {
	String           string
	StringIgnoreCase bool
	Char             rune
	Chars            []rune

	LoopNode *RegexNode
}

type FixedDistanceSet struct {
	Set      *CharSet
	Chars    []rune
	Negated  bool
	Range    *SingleRange
	Distance int
}

type FixedDistanceLiteral struct {
	S        string
	C        rune
	Distance int
}

type RequiredLandmarkChain struct {
	LeadingLoopSet *CharSet
	Landmarks      []RequiredLandmark
}

type RequiredLandmark struct {
	Alternatives []RequiredLandmarkAlternative
}

type RequiredLandmarkAlternative struct {
	Literal                 string
	Chars                   []rune
	Set                     *CharSet
	WhitespaceSet           *CharSet
	MinRepeat               int
	MaxRepeat               int
	RequireWhitespaceBefore bool
	RequireWhitespaceAfter  bool
}

type FindNextStartingPositionMode int

const (
	NoSearch FindNextStartingPositionMode = iota
	// A "beginning" anchor at the beginning of the pattern.
	LeadingAnchor_LeftToRight_Beginning
	// A "start" anchor at the beginning of the pattern.
	LeadingAnchor_LeftToRight_Start
	// An "endz" anchor at the beginning of the pattern.  This is rare.
	LeadingAnchor_LeftToRight_EndZ
	// An "end" anchor at the beginning of the pattern.  This is rare.
	LeadingAnchor_LeftToRight_End
	// A "beginning" anchor at the beginning of the right-to-left pattern.
	LeadingAnchor_RightToLeft_Beginning
	// A "start" anchor at the beginning of the right-to-left pattern.
	LeadingAnchor_RightToLeft_Start
	// An "endz" anchor at the beginning of the right-to-left pattern.  This is rare.
	LeadingAnchor_RightToLeft_EndZ
	// An "end" anchor at the beginning of the right-to-left pattern.  This is rare.
	LeadingAnchor_RightToLeft_End
	// An "end" anchor at the end of the pattern, with the pattern always matching a fixed-length expression.
	TrailingAnchor_FixedLength_LeftToRight_End
	// An "endz" anchor at the end of the pattern, with the pattern always matching a fixed-length expression.
	TrailingAnchor_FixedLength_LeftToRight_EndZ
	// A multi-character substring at the beginning of the pattern.
	LeadingString_LeftToRight
	// A multi-character substring at the beginning of the right-to-left pattern.
	LeadingString_RightToLeft
	// A multi-character ordinal case-insensitive substring at the beginning of the pattern.
	LeadingString_OrdinalIgnoreCase_LeftToRight
	// Multiple leading prefix strings
	LeadingStrings_LeftToRight
	// Multiple leading ordinal case-insensitive prefix strings
	LeadingStrings_OrdinalIgnoreCase_LeftToRight

	// A set starting the pattern.
	LeadingSet_LeftToRight
	// A set starting the right-to-left pattern.
	LeadingSet_RightToLeft

	// A single character at the start of the right-to-left pattern.
	LeadingChar_RightToLeft

	// A single character at a fixed distance from the start of the pattern.
	FixedDistanceChar_LeftToRight
	// A multi-character case-sensitive string at a fixed distance from the start of the pattern.
	FixedDistanceString_LeftToRight

	// One or more sets at a fixed distance from the start of the pattern.
	FixedDistanceSets_LeftToRight

	// A literal (single character, multi-char string, or set with small number of characters) after a non-overlapping set loop at the start of the pattern.
	LiteralAfterLoop_LeftToRight

	// A sequence of required landmarks after a leading loop.
	RequiredLandmarkChain_LeftToRight
)

func newFindOptimizations(tree *RegexTree, opt ParseOptions) *FindOptimizations {
	f := newFindOptimizationsForNode(tree.Root, opt, false)

	if !f.rightToLeft && !f.isUseful() {
		if positiveLookahead, _ := findLeadingPositiveLookahead(tree.Root); positiveLookahead != nil {
			positiveLookaheadOpts := newFindOptimizationsForNode(positiveLookahead.Children[0], opt, true)

			// Lookaheads don't currently factor into the whole-pattern length analysis,
			// as they can overlap with the rest of the expression. Keep the whole-pattern
			// minimum if it's larger, and preserve any max from the original expression.
			positiveLookaheadOpts.MinRequiredLength = max(f.MinRequiredLength, positiveLookaheadOpts.MinRequiredLength)
			positiveLookaheadOpts.MaxPossibleLength = f.MaxPossibleLength
			f = positiveLookaheadOpts
		}
	}

	return f
}

func newFindOptimizationsForNode(root *RegexNode, opt ParseOptions, isLeadingPartial bool) *FindOptimizations {
	f := &FindOptimizations{
		rightToLeft:       opt.RegexOptions&RightToLeft != 0,
		MinRequiredLength: root.ComputeMinLength(),
		LeadingAnchor:     findLeadingOrTrailingAnchor(root, true),
		MaxPossibleLength: -1,
	}

	if f.rightToLeft && f.LeadingAnchor == NtBol {
		// Filter out Bol for RightToLeft, as we don't currently optimize for it.
		f.LeadingAnchor = NtUnknown
	}

	f.FindMode = getFindMode(f.rightToLeft, f.LeadingAnchor)
	if f.FindMode != NoSearch {
		return f
	}

	// Compute any anchor trailing the expression.  If there is one, and we can also compute a fixed length
	// for the whole expression, we can use that to quickly jump to the right location in the input.
	if !f.rightToLeft && !isLeadingPartial {
		f.TrailingAnchor = findLeadingOrTrailingAnchor(root, false)
		if f.TrailingAnchor == NtEnd || f.TrailingAnchor == NtEndZ {
			f.MaxPossibleLength = root.computeMaxLength()
			if f.MinRequiredLength == f.MaxPossibleLength {
				if f.TrailingAnchor == NtEnd {
					f.FindMode = TrailingAnchor_FixedLength_LeftToRight_End
				} else {
					f.FindMode = TrailingAnchor_FixedLength_LeftToRight_EndZ
				}
				return f
			}
		}
	}

	// If there's a leading substring, just use IndexOf and inherit all of its optimizations.
	prefix := findPrefix(root)
	if len(prefix) > 1 {
		f.LeadingPrefix = prefix
		if f.rightToLeft {
			f.FindMode = LeadingString_RightToLeft
		} else {
			f.FindMode = LeadingString_LeftToRight
		}
		return f
	}

	// At this point there are no fast-searchable anchors or case-sensitive prefixes. We can now analyze the
	// pattern for sets and then use any found sets to determine what kind of search to perform.

	// If we're generating code, then the code generation process already handles sets that reduce to a single literal,
	// so we can simplify and just always go for the sets.
	dfa := false                   //(opt&NonBacktracking) != 0
	codeGen := opt.CodeGen && !dfa // for now, we never generate code for NonBacktracking, so treat it as non-codegen
	interpreter := !codeGen && !dfa
	//usesRfoTryFind := !codeGen

	// For interpreter, we want to employ optimizations, but we don't want to make construction significantly
	// more expensive. regexp2cg can opt into more expensive analysis with OptionIsCodeGen. So for the
	// interpreter we focus only on creating a set for the first character. Same for right-to-left, which
	// is used very rarely and thus we don't need to invest in special-casing it.
	if f.rightToLeft {
		// Determine a set for anything that can possibly start the expression.
		set := findFirstCharClass(root)
		if set != nil {
			var chars []rune
			if !set.IsNegated() {
				// See if the set is limited to holding only a few characters.
				chars = set.GetSetChars(5)
			}

			if !codeGen && len(chars) == 1 {
				// The set contains one and only one character, meaning every match starts
				// with the same literal value (potentially case-insensitive). Search for that.
				f.FixedDistanceLiteral.C = chars[0]
				f.FindMode = LeadingChar_RightToLeft
			} else {
				// The set may match multiple characters.  Search for that.
				f.FixedDistanceSets = []FixedDistanceSet{{
					Chars:    chars,
					Set:      set,
					Distance: 0,
				}}
				f.FindMode = LeadingSet_RightToLeft
				f.asciiLookups = make([][]uint, 1)
			}
		}
		return f
	}

	// We're now left-to-right only.

	prefix = findPrefixOrdinalCaseInsensitive(root)
	if len(prefix) > 1 {
		f.LeadingPrefix = prefix
		f.FindMode = LeadingString_OrdinalIgnoreCase_LeftToRight
		return f
	}

	// We're now left-to-right only and looking for multiple prefixes and/or sets.

	// If there are multiple leading strings, we can search for any of them.
	// this works in the interpreter, but we avoid it due to additional cost during construction

	if !interpreter {
		ciPrefixes := findPrefixes(root, true)
		if len(ciPrefixes) > 1 {
			f.LeadingPrefixes = ciPrefixes
			f.FindMode = LeadingStrings_OrdinalIgnoreCase_LeftToRight
			/*SYSTEM_TEXT_REGULAREXPRESSIONS
			if usesRfoTryFind {
						f.LeadingStrings = helpers.NewSearchValues(f.LeadingPrefixes, true)
			}*/
			return f
		}
	}

	// Build up a list of all of the sets that are a fixed distance from the start of the expression.
	fixedDistanceSets := findFixedDistanceSets(root, !interpreter)

	// See if we can make a string of at least two characters long out of those sets.  We should have already caught
	// one at the beginning of the pattern, but there may be one hiding at a non-zero fixed distance into the pattern.
	if len(fixedDistanceSets) > 0 {
		bestFixedDistanceString := findFixedDistanceString(fixedDistanceSets)
		if bestFixedDistanceString != nil {
			f.FindMode = FixedDistanceString_LeftToRight
			f.FixedDistanceLiteral = *bestFixedDistanceString
			return f
		}
	}

	// A landmark chain is more selective than a single leading set for shapes like
	// /name-separator host-separator domain/. Prefer it before falling back to
	// one-position or one-literal candidate searches.
	if landmarkChain := findRequiredLandmarkChain(root); landmarkChain != nil {
		f.FindMode = RequiredLandmarkChain_LeftToRight
		f.LandmarkChain = landmarkChain
		return f
	}

	// As a backup, see if we can find a literal after a leading atomic loop.  That might be better than whatever sets we find, so
	// we want to know whether we have one in our pocket before deciding whether to use a leading set (we'll prefer a leading
	// set if it's something for which we can search efficiently).
	literalAfterLoop := findLiteralFollowingLeadingLoop(root)

	// If we got such sets, we'll likely use them.  However, if the best of them is something that doesn't support an efficient
	// search and we did successfully find a literal after an atomic loop we could search instead, we prefer the efficient search.
	// For example, if we have a negated set, we will still prefer the literal-after-an-atomic-loop because negated sets typically
	// contain _many_ characters (e.g. [^a] is everything but 'a') and are thus more likely to very quickly match, which means any
	// vectorization employed is less likely to kick in and be worth the startup overhead.
	if len(fixedDistanceSets) > 0 {
		// Sort the sets by "quality", such that whatever set is first is the one deemed most efficient to use.
		// In some searches, we may use multiple sets, so we want the subsequent ones to also be the efficiency runners-up.
		slices.SortFunc(fixedDistanceSets, compareFixedDistanceSetsByQuality)

		// If the best fixed-distance set is composed of high-frequency characters, IndexOfAny on
		// those characters is likely to match too many positions. Prefer a case-sensitive
		// multi-prefix search when one is available.
		if !interpreter && !mayContainCaseInsensitiveMatching(root) && hasHighFrequencyChars(fixedDistanceSets[0]) {
			caseSensitivePrefixes := findPrefixes(root, false)
			if len(caseSensitivePrefixes) > 1 {
				f.LeadingPrefixes = caseSensitivePrefixes
				f.FindMode = LeadingStrings_LeftToRight
				return f
			}
		}

		// If there is no literal after the loop, use whatever set we got.
		// If there is a literal after the loop, consider it to be better than a negated set and better than a set with many characters.
		if literalAfterLoop == nil || (len(fixedDistanceSets[0].Chars) > 0 && !fixedDistanceSets[0].Negated) {
			// Determine whether to do searching based on one or more sets or on a single literal. Code-generated engines
			// don't need to special-case literals as they already do codegen to create the optimal lookup based on
			// the set's characteristics.
			if !codeGen && len(fixedDistanceSets) == 1 && len(fixedDistanceSets[0].Chars) == 1 &&
				!fixedDistanceSets[0].Negated {
				f.FixedDistanceLiteral = FixedDistanceLiteral{
					C:        fixedDistanceSets[0].Chars[0],
					Distance: fixedDistanceSets[0].Distance,
				}
				f.FindMode = FixedDistanceChar_LeftToRight
			} else {
				// Limit how many sets we use to avoid doing lots of unnecessary work.  The list was already
				// sorted from best to worst, so just keep the first ones up to our limit.
				const MaxSetsToUse = 3 // arbitrary tuned limit
				if len(fixedDistanceSets) > MaxSetsToUse {
					fixedDistanceSets = fixedDistanceSets[:MaxSetsToUse]
				}

				// Store the sets, and compute which mode to use.
				f.FixedDistanceSets = fixedDistanceSets
				if len(fixedDistanceSets) == 1 && fixedDistanceSets[0].Distance == 0 {
					f.FindMode = LeadingSet_LeftToRight
				} else {
					f.FindMode = FixedDistanceSets_LeftToRight
				}
				f.asciiLookups = make([][]uint, len(fixedDistanceSets))
			}
			return f
		}
	}

	// If we found a literal we can search for after a leading set loop, use it.
	if literalAfterLoop != nil {
		f.FindMode = LiteralAfterLoop_LeftToRight
		f.LiteralAfterLoop = literalAfterLoop
		f.asciiLookups = make([][]uint, 1)
	}

	return f
}

func (f *FindOptimizations) isUseful() bool {
	return f.FindMode != NoSearch || f.LeadingAnchor == NtBol
}

func getFindMode(rtl bool, t NodeType) FindNextStartingPositionMode {
	if rtl {
		switch t {
		case NtBeginning:
			return LeadingAnchor_RightToLeft_Beginning
		case NtStart:
			return LeadingAnchor_RightToLeft_Start
		case NtEnd:
			return LeadingAnchor_RightToLeft_End
		case NtEndZ:
			return LeadingAnchor_RightToLeft_EndZ
		}
	} else {
		switch t {
		case NtBeginning:
			return LeadingAnchor_LeftToRight_Beginning
		case NtStart:
			return LeadingAnchor_LeftToRight_Start
		case NtEnd:
			return LeadingAnchor_LeftToRight_End
		case NtEndZ:
			return LeadingAnchor_LeftToRight_EndZ
		}
	}

	return NoSearch
}

// Analyzes a list of fixed-distance sets to extract a case-sensitive string at a fixed distance.</summary>
func findFixedDistanceString(fixedDistanceSets []FixedDistanceSet) *FixedDistanceLiteral {
	var best *FixedDistanceLiteral

	// A result string must be at least two characters in length; therefore we require at least that many sets.
	if len(fixedDistanceSets) >= 2 {
		// We're walking the sets from beginning to end, so we need them sorted by distance.
		slices.SortFunc(fixedDistanceSets, func(s1, s2 FixedDistanceSet) int {
			return cmp.Compare(s1.Distance, s2.Distance)
		})

		vsb := &bytes.Buffer{}

		// Looking for strings of length >= 2
		start := -1
		for i := 0; i < len(fixedDistanceSets)+1; i++ {
			chars := []rune(nil)
			if i < len(fixedDistanceSets) {
				chars = fixedDistanceSets[i].Chars
			}
			invalidChars := len(chars) != 1 || fixedDistanceSets[i].Negated

			// If the current set ends a sequence (or we've walked off the end), see whether
			// what we've gathered constitues a valid string, and if it's better than the
			// best we've already seen, store it.  Regardless, reset the sequence in order
			// to continue analyzing.
			if invalidChars || (i > 0 && fixedDistanceSets[i].Distance != fixedDistanceSets[i-1].Distance+1) {
				bestLen := 2
				if best != nil {
					bestLen = len(best.S)
				}
				if start != -1 && i-start >= bestLen {
					best = &FixedDistanceLiteral{
						S:        vsb.String(),
						Distance: fixedDistanceSets[start].Distance,
					}
				}

				vsb.Reset()
				start = -1
				if invalidChars {
					continue
				}
			}

			if start == -1 {
				start = i
			}

			vsb.WriteRune(chars[0])
		}
	}

	return best
}
