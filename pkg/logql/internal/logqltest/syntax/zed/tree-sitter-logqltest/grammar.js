/**
 * Tree-sitter grammar for the logqltest declarative DSL (see ../../../README.md).
 *
 * This is the Zed counterpart of ../../logqltest.tmLanguage.json and follows the same design: it
 * covers the DSL only, and the LogQL query on an `eval` line is one opaque token, so LogQL changes
 * never require touching this file.
 *
 * Where the TextMate grammar and the Go parser (../../../parser.go) disagree, this grammar follows
 * the parser: quoted log lines are never unescaped and end at the next quote, `#` comments are cut
 * with the same quote-toggling rule as stripComment, and `{{.i}}` is only special in the log line.
 *
 * The DSL is line-oriented and its blocks are delimited by indentation, so the grammar is a flat
 * sequence of lines (newlines are significant, other whitespace is skipped). It does not model
 * which `load` or `eval` a line belongs to; that is not needed for highlighting.
 */

/// <reference types="tree-sitter-cli/dsl" />
// @ts-check

// Anything up to the next whitespace or comment: a time, a step, an epsilon, a skip target.
const WORD = /[^\s#]+/;

// One character of free-form text, or a whole quoted span. A `#` outside quotes ends the text.
const SPAN = '(?:[^\\s#"`]|"[^"\\n]*"|`[^`\\n]*`)';

// Free-form text to the end of the line, from its first to its last non-space character.
const TEXT = new RegExp(`${SPAN}(?:[ \\t\\r]*${SPAN})*`);

module.exports = grammar({
  name: 'logqltest',

  extras: $ => [/[ \t\r]/, $.comment],

  rules: {
    // Lines are separated, not terminated, so a last line without a trailing newline parses too.
    source_file: $ => seq(optional($._line), repeat(seq($._newline, optional($._line)))),

    _newline: _ => /\n/,

    // Comment-only and blank lines produce no node of their own.
    _line: $ => choice(
      $.clear_command,
      $.load_command,
      $.eval_instant,
      $.eval_range,
      $.eval_select,
      $.expect_command,
      $.skip_command,
      $.stream_line,
      $.scalar_line,
    ),

    comment: _ => /#[^\n]*/,

    clear_command: _ => 'clear',

    // The indented lines that follow are ordinary stream lines.
    load_command: _ => 'load',

    eval_instant: $ => seq(
      'eval', 'instant', 'at', $._time,
      $._query,
    ),

    eval_range: $ => seq(
      'eval', 'range', 'from', $._time, 'to', $._time, 'step', $._time,
      $._query,
    ),

    eval_select: $ => seq(
      'eval', 'select', 'from', $._time, 'to', $._time, $.direction,
      $._query,
    ),

    // The parser matches the direction case-insensitively.
    direction: _ => /[Ff][Oo][Rr][Ww][Aa][Rr][Dd]|[Bb][Aa][Cc][Kk][Ww][Aa][Rr][Dd]/,

    _time: $ => alias($._word, $.duration),

    _word: _ => WORD,

    // The query runs to the end of the line or to a `#` comment. Quoted spans are consumed whole,
    // like the parser's stripComment does, so a `#` inside them does not start a comment.
    _text: _ => TEXT,

    _query: $ => field('query', alias($._text, $.query)),

    expect_command: $ => choice(
      seq('expect', 'empty'),
      seq('expect', 'ordered'),
      seq('expect', 'fail', optional(seq(choice('msg:', 'regex:'), alias($._text, $.failure_text)))),
      seq('expect', 'values-toleration', alias($._word, $.number), 'on', $.stack_name),
    ),

    // Matched loosely like the parser's `\S+` so a typo is reported by the harness, not here.
    skip_command: $ => seq('skip', alias($._word, $.skip_target), 'on', $.stack_name),

    stack_name: _ => /"[^"\n]*"/,

    // `{labels} "line" @ ts [clauses]` or `{labels} p0 p1 ...`: the two are told apart by whether
    // a quote or a sample follows the label set, exactly like the parser's isLogLine.
    stream_line: $ => seq(
      $.label_set,
      choice(
        repeat1($._sample),
        seq($._log_line, $.at_clause, repeat($._clause)),
      ),
    ),

    // The selector ends at the first `}`, as in the parser, so a `}` inside a quoted value ends it.
    label_set: $ => seq('{', optional($.label_set_body), token.immediate('}')),

    label_set_body: _ => token.immediate(/[^}\n]+/),

    _log_line: $ => choice($.string, $.raw_string),

    // Neither form unescapes, so a backslash is plain text and the string ends at the next quote.
    string: $ => seq(
      '"',
      repeat(choice($.template_variable, alias($._string_text, $.string_content))),
      token.immediate('"'),
    ),

    raw_string: $ => seq(
      '`',
      repeat(choice($.template_variable, alias($._raw_text, $.string_content))),
      token.immediate('`'),
    ),

    _string_text: _ => choice(
      token.immediate(/[^"{\n]+/),
      // A `{` that does not start a template variable; the longer template_variable wins otherwise.
      token.immediate(prec(-1, '{')),
    ),

    _raw_text: _ => choice(
      token.immediate(/[^`{\n]+/),
      token.immediate(prec(-1, '{')),
    ),

    template_variable: _ => token.immediate(/\{\{[ \t]*\.[A-Za-z_][A-Za-z0-9_]*[ \t]*\}\}/),

    at_clause: $ => seq('@', alias($._word, $.duration)),

    _clause: $ => choice($.repeat_clause, $.metadata_clause, $.parsed_clause),

    repeat_clause: $ => seq(
      '[', 'repeat', 'every', alias($._clause_word, $.duration), 'for', alias($._clause_word, $.number), ']',
    ),

    // A word inside brackets must not swallow the closing `]`.
    _clause_word: _ => /[^\s\]#]+/,

    metadata_clause: $ => seq('[', 'metadata', repeat($.label_pair), ']'),

    // Result-only: parsed labels come from the query pipeline, so `load` never writes one.
    parsed_clause: $ => seq('[', 'parsed', repeat($.label_pair), ']'),

    label_pair: $ => seq($.label_name, '=', $.label_value),

    // A key is a bare word or, when it contains whitespace, double-quoted.
    label_name: _ => choice(/[^\s"=\]#]+/, /"[^"\n]*"/),

    label_value: _ => /"[^"\n]*"/,

    // An indented bare value: the expected result of a scalar query.
    scalar_line: $ => repeat1($._sample),

    _sample: $ => choice($.gap, $.special_float, $.expansion, $.number),

    gap: _ => '_',

    special_float: _ => /NaN|[+-]?Inf/,

    number: _ => /[+-]?[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?/,

    // `<base>[±<step>]x<count>`, e.g. `2+3x2`. The parts are only valid when written together,
    // so everything after the base is an immediate token.
    expansion: $ => seq(
      $.number,
      optional(alias(token.immediate(prec(1, /[+-][0-9]+(?:\.[0-9]+)?/)), $.number)),
      alias(token.immediate('x'), $.expansion_operator),
      alias(token.immediate(/[0-9]+/), $.number),
    ),
  },
});
