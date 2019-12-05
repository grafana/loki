# Colour terminal text for Go (Golang) [![](https://godoc.org/github.com/alecthomas/colour?status.svg)](http://godoc.org/github.com/alecthomas/colour) [![Build Status](https://travis-ci.org/alecthomas/colour.png)](https://travis-ci.org/alecthomas/colour)

Package colour provides [Quake-style colour formatting][2] for Unix terminals.

The package level functions can be used to write to stdout (or strings or
other files). If stdout is not a terminal, colour formatting will be
stripped.

eg.

    colour.Printf("^0black ^1red ^2green ^3yellow ^4blue ^5magenta ^6cyan ^7white^R\n")


For more control a Printer object can be created with various helper
functions. This can be used to do useful things such as strip formatting,
write to strings, and so on.

The following sequences are converted to their equivalent ANSI colours:

| Sequence | Effect |
| -------- | ------ |
| ^0 | Black |
| ^1 | Red |
| ^2 | Green |
| ^3 | Yellow |
| ^4 | Blue |
| ^5 | Cyan (light blue) |
| ^6 | Magenta (purple) |
| ^7 | White |
| ^8 | Black Background |
| ^9 | Red Background |
| ^a | Green Background |
| ^b | Yellow Background |
| ^c | Blue Background |
| ^d | Cyan (light blue) Background |
| ^e | Magenta (purple) Background |
| ^f | White Background |
| ^R | Reset |
| ^U | Underline |
| ^D | Dim |
| ^B | Bold |
| ^S | Strikethrough |

[1]: http://godoc.org/github.com/alecthomas/colour
[2]: http://www.holysh1t.net/quake-live-colors-nickname/
