
//line pkg/logql/log/pattern/lexer.rl:1
package pattern


//line pkg/logql/log/pattern/lexer.rl.go:7
var _pattern_actions []byte = []byte{
	0, 1, 0, 1, 1, 1, 2, 1, 3, 
	1, 4, 1, 5, 1, 6, 
}

var _pattern_key_offsets []byte = []byte{
	0, 8, 9, 
}

var _pattern_trans_keys []byte = []byte{
	62, 95, 48, 57, 65, 90, 97, 122, 
	60, 95, 65, 90, 97, 122, 
}

var _pattern_single_lengths []byte = []byte{
	2, 1, 1, 
}

var _pattern_range_lengths []byte = []byte{
	3, 0, 2, 
}

var _pattern_index_offsets []byte = []byte{
	0, 6, 8, 
}

var _pattern_trans_targs []byte = []byte{
	1, 0, 0, 0, 0, 1, 2, 1, 
	0, 0, 0, 1, 1, 1, 
}

var _pattern_trans_actions []byte = []byte{
	7, 0, 0, 0, 0, 13, 5, 9, 
	0, 0, 0, 11, 13, 11, 
}

var _pattern_to_state_actions []byte = []byte{
	0, 1, 0, 
}

var _pattern_from_state_actions []byte = []byte{
	0, 3, 0, 
}

var _pattern_eof_trans []byte = []byte{
	13, 0, 14, 
}

const pattern_start int = 1
const pattern_first_final int = 1
const pattern_error int = -1

const pattern_en_main int = 1


//line pkg/logql/log/pattern/lexer.rl:14


const LEXER_ERROR = 0


//line pkg/logql/log/pattern/lexer.rl:21


func (lex *lexer) Lex(out *exprSymType) int {
    eof := lex.pe
    tok := 0

    
//line pkg/logql/log/pattern/lexer.rl.go:77
	{
	var _klen int
	var _trans int
	var _acts int
	var _nacts uint
	var _keys int
	if ( lex.p) == ( lex.pe) {
		goto _test_eof
	}
_resume:
	_acts = int(_pattern_from_state_actions[ lex.cs])
	_nacts = uint(_pattern_actions[_acts]); _acts++
	for ; _nacts > 0; _nacts-- {
		 _acts++
		switch _pattern_actions[_acts - 1] {
		case 1:
//line NONE:1
 lex.ts = ( lex.p)

//line pkg/logql/log/pattern/lexer.rl.go:97
		}
	}

	_keys = int(_pattern_key_offsets[ lex.cs])
	_trans = int(_pattern_index_offsets[ lex.cs])

	_klen = int(_pattern_single_lengths[ lex.cs])
	if _klen > 0 {
		_lower := int(_keys)
		var _mid int
		_upper := int(_keys + _klen - 1)
		for {
			if _upper < _lower {
				break
			}

			_mid = _lower + ((_upper - _lower) >> 1)
			switch {
			case  lex.data[( lex.p)] < _pattern_trans_keys[_mid]:
				_upper = _mid - 1
			case  lex.data[( lex.p)] > _pattern_trans_keys[_mid]:
				_lower = _mid + 1
			default:
				_trans += int(_mid - int(_keys))
				goto _match
			}
		}
		_keys += _klen
		_trans += _klen
	}

	_klen = int(_pattern_range_lengths[ lex.cs])
	if _klen > 0 {
		_lower := int(_keys)
		var _mid int
		_upper := int(_keys + (_klen << 1) - 2)
		for {
			if _upper < _lower {
				break
			}

			_mid = _lower + (((_upper - _lower) >> 1) & ^1)
			switch {
			case  lex.data[( lex.p)] < _pattern_trans_keys[_mid]:
				_upper = _mid - 2
			case  lex.data[( lex.p)] > _pattern_trans_keys[_mid + 1]:
				_lower = _mid + 2
			default:
				_trans += int((_mid - int(_keys)) >> 1)
				goto _match
			}
		}
		_trans += _klen
	}

_match:
_eof_trans:
	 lex.cs = int(_pattern_trans_targs[_trans])

	if _pattern_trans_actions[_trans] == 0 {
		goto _again
	}

	_acts = int(_pattern_trans_actions[_trans])
	_nacts = uint(_pattern_actions[_acts]); _acts++
	for ; _nacts > 0; _nacts-- {
		_acts++
		switch _pattern_actions[_acts-1] {
		case 2:
//line NONE:1
 lex.te = ( lex.p)+1

		case 3:
//line pkg/logql/log/pattern/lexer.rl:30
 lex.te = ( lex.p)+1
{ tok = lex.handle(lex.identifier(out)); ( lex.p)++; goto _out
 }
		case 4:
//line pkg/logql/log/pattern/lexer.rl:31
 lex.te = ( lex.p)+1
{ tok = lex.handle(lex.literal(out)); ( lex.p)++; goto _out
 }
		case 5:
//line pkg/logql/log/pattern/lexer.rl:31
 lex.te = ( lex.p)
( lex.p)--
{ tok = lex.handle(lex.literal(out)); ( lex.p)++; goto _out
 }
		case 6:
//line pkg/logql/log/pattern/lexer.rl:31
( lex.p) = ( lex.te) - 1
{ tok = lex.handle(lex.literal(out)); ( lex.p)++; goto _out
 }
//line pkg/logql/log/pattern/lexer.rl.go:191
		}
	}

_again:
	_acts = int(_pattern_to_state_actions[ lex.cs])
	_nacts = uint(_pattern_actions[_acts]); _acts++
	for ; _nacts > 0; _nacts-- {
		_acts++
		switch _pattern_actions[_acts-1] {
		case 0:
//line NONE:1
 lex.ts = 0

//line pkg/logql/log/pattern/lexer.rl.go:205
		}
	}

	( lex.p)++
	if ( lex.p) != ( lex.pe) {
		goto _resume
	}
	_test_eof: {}
	if ( lex.p) == eof {
		if _pattern_eof_trans[ lex.cs] > 0 {
			_trans = int(_pattern_eof_trans[ lex.cs] - 1)
			goto _eof_trans
		}
	}

	_out: {}
	}

//line pkg/logql/log/pattern/lexer.rl:35


    return tok;
}


func (lex *lexer) init() {
    
//line pkg/logql/log/pattern/lexer.rl.go:233
	{
	 lex.cs = pattern_start
	 lex.ts = 0
	 lex.te = 0
	 lex.act = 0
	}

//line pkg/logql/log/pattern/lexer.rl:43
}
