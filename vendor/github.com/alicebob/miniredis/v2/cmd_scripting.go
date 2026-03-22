package miniredis

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/parse"

	luajson "github.com/alicebob/miniredis/v2/gopher-json"
	"github.com/alicebob/miniredis/v2/server"
)

func commandsScripting(m *Miniredis) {
	m.srv.Register("EVAL", m.cmdEval)
	m.srv.Register("EVAL_RO", m.cmdEvalro, server.ReadOnlyOption())
	m.srv.Register("EVALSHA", m.cmdEvalsha)
	m.srv.Register("EVALSHA_RO", m.cmdEvalshaRo, server.ReadOnlyOption())
	m.srv.Register("SCRIPT", m.cmdScript)
}

var (
	parsedScripts = sync.Map{}
)

// Execute lua. Needs to run m.Lock()ed, from within withTx().
// Returns true if the lua was OK (and hence should be cached).
func (m *Miniredis) runLuaScript(c *server.Peer, sha, script string, readOnly bool, args []string) bool {
	l := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer l.Close()

	// Taken from the go-lua manual
	for _, pair := range []struct {
		n string
		f lua.LGFunction
	}{
		{lua.LoadLibName, lua.OpenPackage},
		{lua.BaseLibName, lua.OpenBase},
		{lua.CoroutineLibName, lua.OpenCoroutine},
		{lua.TabLibName, lua.OpenTable},
		{lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath},
		{lua.DebugLibName, lua.OpenDebug},
	} {
		if err := l.CallByParam(lua.P{
			Fn:      l.NewFunction(pair.f),
			NRet:    0,
			Protect: true,
		}, lua.LString(pair.n)); err != nil {
			panic(err)
		}
	}

	luajson.Preload(l)
	requireGlobal(l, "cjson", "json")

	// set global variable KEYS
	keysTable := l.NewTable()
	keysS, args := args[0], args[1:]
	keysLen, err := strconv.Atoi(keysS)
	if err != nil {
		c.WriteError(msgInvalidInt)
		return false
	}
	if keysLen < 0 {
		c.WriteError(msgNegativeKeysNumber)
		return false
	}
	if keysLen > len(args) {
		c.WriteError(msgInvalidKeysNumber)
		return false
	}
	keys, args := args[:keysLen], args[keysLen:]
	for i, k := range keys {
		l.RawSet(keysTable, lua.LNumber(i+1), lua.LString(k))
	}
	l.SetGlobal("KEYS", keysTable)

	argvTable := l.NewTable()
	for i, a := range args {
		l.RawSet(argvTable, lua.LNumber(i+1), lua.LString(a))
	}
	l.SetGlobal("ARGV", argvTable)

	redisFuncs, redisConstants := mkLua(m.srv, c, sha, readOnly)
	// Register command handlers
	l.Push(l.NewFunction(func(l *lua.LState) int {
		mod := l.RegisterModule("redis", redisFuncs).(*lua.LTable)
		for k, v := range redisConstants {
			mod.RawSetString(k, v)
		}
		l.Push(mod)
		return 1
	}))
	l.RegisterModule("os", mkLuaOS())

	_ = doScript(l, protectGlobals)

	l.Push(lua.LString("redis"))
	l.Call(1, 0)

	// lua can call redis.setresp(...), but it's tmp state.
	oldresp := c.Resp3
	if err := doScript(l, script); err != nil {
		c.WriteError(err.Error())
		return false
	}

	luaToRedis(l, c, l.Get(1))
	c.Resp3 = oldresp
	c.SwitchResp3 = nil
	return true
}

// doScript pre-compiles the given script into a Lua prototype,
// then executes the pre-compiled function against the given lua state.
//
// This is thread-safe.
func doScript(l *lua.LState, script string) error {
	proto, err := compile(script)
	if err != nil {
		return fmt.Errorf(errLuaParseError(err))
	}

	lfunc := l.NewFunctionFromProto(proto)
	l.Push(lfunc)
	if err := l.PCall(0, lua.MultRet, nil); err != nil {
		// ensure we wrap with the correct format.
		return fmt.Errorf(errLuaParseError(err))
	}

	return nil
}

func compile(script string) (*lua.FunctionProto, error) {
	if val, ok := parsedScripts.Load(script); ok {
		return val.(*lua.FunctionProto), nil
	}
	chunk, err := parse.Parse(strings.NewReader(script), "<string>")
	if err != nil {
		return nil, err
	}
	proto, err := lua.Compile(chunk, "")
	if err != nil {
		return nil, err
	}
	parsedScripts.Store(script, proto)
	return proto, nil
}

// Shared implementation for EVAL and EVALRO
func (m *Miniredis) cmdEvalShared(c *server.Peer, cmd string, readOnly bool, args []string) {
	if !m.isValidCMD(c, cmd, args, atLeast(2)) {
		return
	}

	ctx := getCtx(c)
	if ctx.nested {
		c.WriteError(msgNotFromScripts(ctx.nestedSHA))
		return
	}

	script, args := args[0], args[1:]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		sha := sha1Hex(script)
		ok := m.runLuaScript(c, sha, script, readOnly, args)
		if ok {
			m.scripts[sha] = script
		}
	})
}

// Wrapper function for EVAL command
func (m *Miniredis) cmdEval(c *server.Peer, cmd string, args []string) {
	m.cmdEvalShared(c, cmd, false, args)
}

// Shared implementation for EVALSHA and EVALSHA_RO
func (m *Miniredis) cmdEvalshaShared(c *server.Peer, cmd string, readOnly bool, args []string) {
	if !m.isValidCMD(c, cmd, args, atLeast(2)) {
		return
	}

	ctx := getCtx(c)
	if ctx.nested {
		c.WriteError(msgNotFromScripts(ctx.nestedSHA))
		return
	}

	sha, args := args[0], args[1:]

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		script, ok := m.scripts[sha]
		if !ok {
			c.WriteError(msgNoScriptFound)
			return
		}

		m.runLuaScript(c, sha, script, readOnly, args)
	})
}

// Wrapper function for EVALSHA command
func (m *Miniredis) cmdEvalsha(c *server.Peer, cmd string, args []string) {
	m.cmdEvalshaShared(c, cmd, false, args)
}

// Wrapper function for EVALRO command
func (m *Miniredis) cmdEvalro(c *server.Peer, cmd string, args []string) {
	m.cmdEvalShared(c, cmd, true, args)
}

// Wrapper function for EVALSHA_RO command
func (m *Miniredis) cmdEvalshaRo(c *server.Peer, cmd string, args []string) {
	m.cmdEvalshaShared(c, cmd, true, args)
}

func (m *Miniredis) cmdScript(c *server.Peer, cmd string, args []string) {
	if !m.isValidCMD(c, cmd, args, atLeast(1)) {
		return
	}

	ctx := getCtx(c)
	if ctx.nested {
		c.WriteError(msgNotFromScripts(ctx.nestedSHA))
		return
	}

	var opts struct {
		subcmd string
		script string
	}

	opts.subcmd, args = args[0], args[1:]

	switch strings.ToLower(opts.subcmd) {
	case "load":
		if len(args) != 1 {
			setDirty(c)
			c.WriteError(fmt.Sprintf(msgFScriptUsage, "LOAD"))
			return
		}
		opts.script = args[0]
	case "exists":
		if len(args) == 0 {
			setDirty(c)
			c.WriteError(errWrongNumber("script|exists"))
			return
		}
	case "flush":
		if len(args) == 1 {
			switch strings.ToUpper(args[0]) {
			case "SYNC", "ASYNC":
				args = args[1:]
			default:
			}
		}
		if len(args) != 0 {
			setDirty(c)
			c.WriteError(msgScriptFlush)
			return
		}
	default:
		setDirty(c)
		c.WriteError(fmt.Sprintf(msgFScriptUsageSimple, strings.ToUpper(opts.subcmd)))
		return
	}

	withTx(m, c, func(c *server.Peer, ctx *connCtx) {
		switch strings.ToLower(opts.subcmd) {
		case "load":
			if _, err := parse.Parse(strings.NewReader(opts.script), "user_script"); err != nil {
				c.WriteError(errLuaParseError(err))
				return
			}
			sha := sha1Hex(opts.script)
			m.scripts[sha] = opts.script
			c.WriteBulk(sha)
		case "exists":
			c.WriteLen(len(args))
			for _, arg := range args {
				if _, ok := m.scripts[arg]; ok {
					c.WriteInt(1)
				} else {
					c.WriteInt(0)
				}
			}
		case "flush":
			m.scripts = map[string]string{}
			c.WriteOK()
		}
	})
}

func sha1Hex(s string) string {
	h := sha1.New()
	io.WriteString(h, s)
	return hex.EncodeToString(h.Sum(nil))
}

// requireGlobal imports module modName into the global namespace with the
// identifier id.  panics if an error results from the function execution
func requireGlobal(l *lua.LState, id, modName string) {
	if err := l.CallByParam(lua.P{
		Fn:      l.GetGlobal("require"),
		NRet:    1,
		Protect: true,
	}, lua.LString(modName)); err != nil {
		panic(err)
	}
	mod := l.Get(-1)
	l.Pop(1)

	l.SetGlobal(id, mod)
}

// the following script protects globals
// it is based on:  http://metalua.luaforge.net/src/lib/strict.lua.html
var protectGlobals = `
local dbg=debug
local mt = {}
setmetatable(_G, mt)
mt.__newindex = function (t, n, v)
  if dbg.getinfo(2) then
    local w = dbg.getinfo(2, "S").what
    if w ~= "C" then
      error("Script attempted to create global variable '"..tostring(n).."'", 2)
    end
  end
  rawset(t, n, v)
end
mt.__index = function (t, n)
  if dbg.getinfo(2) and dbg.getinfo(2, "S").what ~= "C" then
    error("Script attempted to access nonexistent global variable '"..tostring(n).."'", 2)
  end
  return rawget(t, n)
end
debug = nil

`
