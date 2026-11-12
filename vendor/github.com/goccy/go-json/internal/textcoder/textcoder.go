// Package textcoder lets the v2 json package give a jsontext.Encoder to a method or a function of marshaling,
// which writes after the output of the encoder of the v2 semantics: jsontext sets these functions, which take its
// encoder as an any, since this package can't import jsontext.
package textcoder

import (
	"io"
	"unsafe"

	"github.com/goccy/go-json/internal/options"
)

// Level is a level of the output: the top level, or an open object or array.
type Level struct {
	Object bool
	// Count is the number of the tokens of the level written before the value of the next level, which is open,
	// or, at the innermost level, before the next token: the elements of an array, the names and the values of an
	// object.
	Count int64
	// Name is the last name of an object, unquoted: of the value which is open or next, if Count is odd. The names
	// before it are not kept: a method writes a single value, which is checked for duplicate names in itself, and
	// the place of a value is the last name of each object. It is nil where the level has no name, and empty but
	// not nil for the name "".
	Name []byte
}

// Attachment is the place of the output where an encoder is attached ( see Attach ), and, after Attach and Detach,
// where it is.
type Attachment struct {
	// Out is the output before the place, at the offset Base of the whole output, or, if Out is empty, Base is
	// the offset of the value after its delimiter: the delimiter which the encoder writes before the value, whose
	// length Attach sets in Skip, is not a part of the output then, which the caller writes to another encoder.
	Out  []byte
	Base int64
	// Object and Count are the innermost level of the place, an open object or array, or the top level if Top is
	// set: Count has the parity of the place, a value or a name, and is 0 only if the level has no token yet. The
	// levels around it, and its name and count, Outer gives when the encoder needs them.
	Object bool
	Top    bool
	Count  int64
	Outer  Outer
	// Opts are the options of the encoder, which are the ones it was attached with the last time if Same is set.
	Opts *options.Config
	Same bool
	// Skip, and Depth and Count of the innermost level, which Detach sets after the writes, Attach sets.
	Skip  int
	Depth int
	// OuterDepth is the number of the arrays and the objects which are open around the place in the whole output,
	// which the encoder counts its levels after: it fails to begin one past the deepest level, as the encoder of the
	// whole output does.
	OuterDepth int
	// Failed is the error of a call which failed at the encoder, which Detach sets ( see Fail ).
	Failed error
}

// Outer gives the levels of the place where an encoder was attached, the top level first ( see Attach ): they are
// found only when the encoder needs them, for a JSON pointer or the stack, which most writes don't.
type Outer interface {
	Levels() []Level
}

var (
	// Attach makes enc, a *jsontext.Encoder, write after the output of the place p by its options ( see
	// Attachment ).
	// enc is the pointer itself, without the check of a type assertion, which the calls for every value of a
	// method don't take; the place is given by a pointer, as its fields don't fit the registers of a call.
	Attach func(enc unsafe.Pointer, p *Attachment)
	// Detach sets the output of enc, which Attach set up, and its depth and the count of the tokens of its
	// innermost level in p.
	Detach func(enc unsafe.Pointer, p *Attachment)
	// Position returns the levels of enc, a *jsontext.Encoder, the top level first, and the offset in its output
	// where its next value starts: after the delimiter and the white space before it.
	Position func(enc any, levels []Level) ([]Level, int64)
	// Place returns the innermost level of enc, whose count may be the one which Attach was given ( see Attach ),
	// whether it is the top level, the offset in its output where its next value starts, the Outer which it was
	// attached with, if it was, and the number of the arrays and the objects open around its next value in the whole
	// output ( see Attachment.OuterDepth ).
	Place func(enc any) (inner Level, top bool, offset int64, outer Outer, depth int)
	// Configure makes enc write by the options until restore is called, which gives it back its own, with the
	// strings of the values as they are, which the calls which wrote them escaped.
	Configure func(enc any, opts options.Options) (restore func())
	// Invalidate makes enc fail every write until a Reset: MarshalEncode stopped within an object or an array it
	// opened.
	Invalidate func(enc any)
	// Fail records err in enc, an encoder which Attach attached, the first error of a call of MarshalEncode which
	// failed at it: the value which enc was given the place of fails with it, whatever its method returns.
	Fail func(enc any, err error)
)

// DecoderAttachment is the place of the input where a decoder is attached ( see AttachDecoder ), and, after
// AttachDecoder and DetachDecoder, where it stopped reading.
type DecoderAttachment struct {
	// In is the input which the decoder reads from Pos: the end of the token before the value, so that it reads the
	// delimiter before the value as its own. In[0] is at the offset Base of the whole input, which the offsets of
	// the decoder count from. R is the reader of the input after In, which the decoder reads when it needs more, or
	// nil if In has all of it: the whole input of the call, or of the value. If Own is set, In is a buffer of the
	// caller, which the decoder moves its input in and reads into, rather than one it makes, and which is In after
	// DetachDecoder, grown if the decoder needed more room, for the next attach.
	In   []byte
	Pos  int
	Base int64
	R    io.Reader
	Own  bool
	// Object and Count are the innermost level of the place, an open object or array, or the top level if Top is
	// set: Count has the parity of the place, which is a value, and is 0 only if the level has no token before
	// it. The levels around it, and its name and its count, Outer gives when the decoder needs them.
	Object bool
	Top    bool
	Count  int64
	Outer  Outer
	// OuterDepth is the number of the arrays and the objects which are open around the place in the whole input,
	// which the decoder counts its levels after: it fails to begin one past the deepest level, as the decoder of
	// the whole input does.
	OuterDepth int
	// Opts are the options of the decoder, which are the ones it was attached with the last time if Same is set.
	Opts *options.Config
	Same bool
	// Depth and Count of the innermost level, Pos, End, the offset in the whole input where the decoder stopped
	// reading, and PrevStart, the one of the start of the last token or value which was read, DetachDecoder sets
	// after the reads; Depth is the one of the place after AttachDecoder. If the decoder read R, Rest is what it
	// read and didn't use, which follows End, and Pos is not a place of In.
	Depth     int
	End       int64
	PrevStart int64
	Rest      []byte
}

var (
	// AttachDecoder makes dec, a *jsontext.Decoder, read the input of the place p by its options ( see
	// DecoderAttachment ).
	AttachDecoder func(dec unsafe.Pointer, p *DecoderAttachment)
	// DetachDecoder sets where dec, which AttachDecoder set up, stopped reading in p, and drops its input: a
	// decoder kept past its call reads nothing more.
	DetachDecoder func(dec unsafe.Pointer, p *DecoderAttachment)
	// DecoderPointer returns the JSON pointer of the place of dec, which DetachDecoder detached, which where points
	// to: the last value which was read (-1), the next value (+1), or the level it is in (0).
	DecoderPointer func(dec unsafe.Pointer, where int) string
	// ReleaseDecoder drops the options of dec, which it was attached with, and its outer levels: the decoder, which
	// is kept for the next call, must not keep the functions of the caller.
	ReleaseDecoder func(dec unsafe.Pointer)
)

// InputRoom is the room which a decoder of a reader leaves after the input in its buffer, for a stream of the decoders
// of v2 which reads the buffer in place ( see DecoderBuffer ): the padding which the stream keeps after the input in
// its own buffer, for the nul byte after the input and a block of 64 bytes which a scan reads from any byte of the
// input, past it from the last bytes.
const InputRoom = 64

// The decoding of the next value of a *jsontext.Decoder by the v2 json package ( UnmarshalDecode ), which reads the
// value itself from the input of the decoder, and then makes the decoder go on after it.
var (
	// DecoderPlace returns the levels of the whole input around the next value of dec, the top level first, appended
	// to levels ( see Level ), whose names are copied into the arrays of the names of the entries of levels after its
	// length, which are reused.
	DecoderPlace func(dec unsafe.Pointer, levels []Level) []Level
	// DecoderAt returns the rest of the place of the next value of dec: whether it is a name of an object, the Outer
	// of the attach of dec, or nil, and the number of the arrays and the objects open around it.
	DecoderAt func(dec unsafe.Pointer) (bool, Outer, int)
	// BeginValue reads what dec reads before its next value, which is not a name, as ReadValue reads it: the
	// white space and the delimiter, which it checks, and returns the offset of the value in the whole input, or
	// the error which ReadValue returns there, io.EOF after the last value of the input. The value isn't read.
	BeginValue func(dec unsafe.Pointer) (int64, error)
	// DecoderInput returns the input of dec from the offset start of the whole input, which its buffer has: the
	// bytes of its buffer, and its reader, which reads the input after them.
	DecoderInput func(dec unsafe.Pointer, start int64) ([]byte, io.Reader)
	// DecoderBuffer returns the buffer of dec, which its input up to len(buf) has been read into, and the offset of
	// its first byte in the whole input. It reports false if a caller can't read the buffer in place and read more
	// into it ( see FetchInput ): the array of a bytes.Buffer, of a decoder of a bytes.Buffer, which the decoder
	// doesn't own, or a buffer without InputRoom bytes of room after its input, which a decoder of a reader leaves
	// it.
	DecoderBuffer func(dec unsafe.Pointer) ([]byte, int64, bool)
	// FetchInput reads more of the input of dec into its buffer, which DecoderBuffer gave a caller, after what has
	// been read, n bytes at most, by one read of its reader, keeping the bytes from the offset keep of the whole
	// input, which the buffer may move to its start, and returns the buffer as DecoderBuffer does, the number of
	// the bytes read and the error of the read, which dec doesn't keep. The state of dec is the caller's until it
	// ends the value by EndValue, with the rest, or PutBack.
	FetchInput func(dec unsafe.Pointer, keep int64, n int) ([]byte, int64, int, error)
	// EndValue makes dec go on after the next value, which was read from start to end, offsets of the whole
	// input: in its buffer, which may have dropped the start of the value for a caller which read more into it
	// ( see FetchInput ), or, if rest is set, from rest, the input after the value which was read from its reader,
	// which it copies. The value is the last value which dec read, from start, whose place a decoder attached at a
	// value reports to the caller ( see DetachDecoder ).
	EndValue func(dec unsafe.Pointer, start, end int64, rest []byte)
	// PutBack makes dec read in, the input from the offset start of the whole input, which a caller read from its
	// reader, as the next input: dec is where it was before its next value, which the caller didn't decode.
	PutBack func(dec unsafe.Pointer, start int64, in []byte)
	// FailDecoder makes every later read of dec fail with err, which the read returns as it is.
	FailDecoder func(dec unsafe.Pointer, err error)
	// DecoderFailed reports whether FailDecoder made every read of dec fail.
	DecoderFailed func(dec unsafe.Pointer) bool
	// SkipToEnd reads the input of the place p as UnmarshalPlaceAt reads it, up to the end of the value of the
	// level of the place at depth, which has count tokens before the value: the levels deeper are read to their
	// ends. It returns the offset of the end of the value, and what it read from the reader of p and didn't use,
	// or the error of the input.
	SkipToEnd func(p *DecoderAttachment, depth int, count int64) (int64, []byte, error)
)

// Read is how UnmarshalPlace reads the value which it stops at.
type Read uint8

const (
	// ReadToken reads the first token of the value: all of it but for an object or an array.
	ReadToken Read = iota
	// ReadValue reads the whole value.
	ReadValue
	// ReadNothing reads nothing of the value.
	ReadNothing
)

// UnmarshalPlace reads in, the input of an unmarshaling of the v2 json package, by the options, as
// encoding/json/v2 reads it, up to the value which starts at stop, which it reads as read says: it returns the
// syntax error it meets first, a *jsontext.SyntacticError, or else the JSON pointer of the value. With stop -1,
// it reads the whole input and returns its first syntax error, or nil if it has none.
var UnmarshalPlace func(in []byte, opts *options.Config, stop int64, read Read) (string, error)

// UnmarshalPlaceAt is UnmarshalPlace for a part of the input, p.In, which is read from the place p as a decoder
// attached there reads it ( see DecoderAttachment ), with the offsets of the whole input: it reads up to the value
// which starts at the offset stop, or, with stop -1, up to the first syntax error, and returns "" and nil if In
// ends before either. The levels of the place may end in the part, as no method reads it.
var UnmarshalPlaceAt func(p *DecoderAttachment, stop int64, read Read) (string, error)

// AfterTopLevelError returns the syntax error of the first byte of in, at the offset base of the input, which follows
// the top-level value, as jsontext reports it.
var AfterTopLevelError func(in []byte, base int64, opts *options.Config) error

// ConfigureDecoder makes the Decoder at dec read by the options of opts, which are its own and the ones of a call,
// until restore is called, which gives it back its own.
var ConfigureDecoder func(dec unsafe.Pointer, opts *options.Config) (restore func())

// ReadError returns err, the error of a reader of the input, as jsontext reports it, and IsIOError reports whether
// err is the error of a reader or of a writer, which a method passes on as it is.
var (
	ReadError func(err error) error
	IsIOError func(err error) bool
)

// A skipper is a scanner of jsontext which reads the values that a call of the v2 json package skips, by the options
// of the names of an object twice and of invalid UTF-8, without writing them: TakeSkipper returns one, which the
// call keeps for all its values, and ReleaseSkipper puts it back.
//
// ValidValue reports whether v, a JSON value valid by the grammar, has the text which jsontext reads by the options
// of the skipper: valid UTF-8 unless invalid UTF-8 is allowed, and no object with a name twice unless that is.
var (
	TakeSkipper    func(allowDuplicateNames, allowInvalidUTF8 bool) unsafe.Pointer
	ReleaseSkipper func(skipper unsafe.Pointer)
	ValidValue     func(skipper unsafe.Pointer, v []byte) bool
)
