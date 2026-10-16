; Capture names follow Zed's theme vocabulary. Later patterns win over earlier ones on the same
; node, so the generic captures come first.

(comment) @comment

; Commands and their words. `clear` and `load` are whole-line commands, so each is a single token.
(clear_command) @keyword
(load_command) @keyword

[
  "eval"
  "instant"
  "range"
  "select"
  "at"
  "from"
  "to"
  "step"
  "expect"
  "fail"
  "empty"
  "ordered"
  "values-toleration"
  "skip"
  "on"
  "repeat"
  "every"
  "for"
  "metadata"
  "parsed"
  "msg:"
  "regex:"
] @keyword

(skip_target) @keyword
(direction) @constant

; The LogQL query is one uniform span, as in the TextMate grammar.
(query) @string
(failure_text) @string
(stack_name) @string

; Stream selectors and expected label sets.
(label_set) @tag

; Log lines.
(string) @string
(raw_string) @string
(template_variable) @variable.special

; `@ <ts>`, `[repeat ...]`, `[metadata ...]` and `[parsed ...]`.
"@" @operator
(duration) @number
(label_name) @property
"=" @operator
(label_value) @string
["[" "]"] @punctuation.bracket

; Expected values.
(number) @number
(gap) @constant
(special_float) @constant
(expansion_operator) @operator
