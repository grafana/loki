"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = loadCodeDefault;
exports.supportsESM = void 0;
var _async3 = require("../../gensync-utils/async.js");
function _path() {
  const data = require("path");
  _path = function () {
    return data;
  };
  return data;
}
function _url() {
  const data = require("url");
  _url = function () {
    return data;
  };
  return data;
}
function _semver() {
  const data = require("semver");
  _semver = function () {
    return data;
  };
  return data;
}
function _debug() {
  const data = require("debug");
  _debug = function () {
    return data;
  };
  return data;
}
var _rewriteStackTrace = require("../../errors/rewrite-stack-trace.js");
var _configError = require("../../errors/config-error.js");
var _transformFile = require("../../transform-file.js");
function asyncGeneratorStep(n, t, e, r, o, a, c) { try { var i = n[a](c), u = i.value; } catch (n) { return void e(n); } i.done ? t(u) : Promise.resolve(u).then(r, o); }
function _asyncToGenerator(n) { return function () { var t = this, e = arguments; return new Promise(function (r, o) { var a = n.apply(t, e); function _next(n) { asyncGeneratorStep(a, r, o, _next, _throw, "next", n); } function _throw(n) { asyncGeneratorStep(a, r, o, _next, _throw, "throw", n); } _next(void 0); }); }; }
const debug = _debug()("babel:config:loading:files:module-types");
{
  try {
    var import_ = require("./import.cjs");
  } catch (_unused) {}
}
const supportsESM = exports.supportsESM = _semver().satisfies(process.versions.node, "^12.17 || >=13.2");
const LOADING_CJS_FILES = new Set();
function loadCjsDefault(filepath) {
  if (LOADING_CJS_FILES.has(filepath)) {
    debug("Auto-ignoring usage of config %o.", filepath);
    return {};
  }
  let module;
  try {
    LOADING_CJS_FILES.add(filepath);
    module = (0, _rewriteStackTrace.endHiddenCallStack)(require)(filepath);
  } finally {
    LOADING_CJS_FILES.delete(filepath);
  }
  {
    return module != null && (module.__esModule || module[Symbol.toStringTag] === "Module") ? module.default || (arguments[1] ? module : undefined) : module;
  }
}
const loadMjsFromPath = (0, _rewriteStackTrace.endHiddenCallStack)(function () {
  var _loadMjsFromPath = _asyncToGenerator(function* (filepath) {
    const url = (0, _url().pathToFileURL)(filepath).toString() + "?import";
    {
      if (!import_) {
        throw new _configError.default("Internal error: Native ECMAScript modules aren't supported by this platform.\n", filepath);
      }
      return yield import_(url);
    }
  });
  function loadMjsFromPath(_x) {
    return _loadMjsFromPath.apply(this, arguments);
  }
  return loadMjsFromPath;
}());
const SUPPORTED_EXTENSIONS = new Set([".js", ".mjs", ".cjs", ".cts"]);
const asyncModules = new Set();
function* loadCodeDefault(filepath, loader, esmError, tlaError) {
  var _async2;
  let async;
  let ext = _path().extname(filepath);
  if (!SUPPORTED_EXTENSIONS.has(ext)) ext = ".js";
  const pattern = `${loader} ${ext}`;
  switch (pattern) {
    case "require .cjs":
    case "auto .cjs":
      {
        return loadCjsDefault(filepath, arguments[2]);
      }
    case "require .cts":
    case "auto .cts":
      return loadCtsDefault(filepath);
    case "auto .js":
    case "require .js":
    case "require .mjs":
      try {
        {
          return loadCjsDefault(filepath, arguments[2]);
        }
      } catch (e) {
        if (e.code === "ERR_REQUIRE_ASYNC_MODULE" || e.code === "ERR_REQUIRE_CYCLE_MODULE" && asyncModules.has(filepath)) {
          var _async;
          asyncModules.add(filepath);
          if (!((_async = async) != null ? _async : async = yield* (0, _async3.isAsync)())) {
            throw new _configError.default(tlaError, filepath);
          }
        } else if (e.code === "ERR_REQUIRE_ESM" || ext === ".mjs") {} else {
          throw e;
        }
      }
    case "auto .mjs":
      if ((_async2 = async) != null ? _async2 : async = yield* (0, _async3.isAsync)()) {
        return (yield* (0, _async3.waitFor)(loadMjsFromPath(filepath))).default;
      }
      throw new _configError.default(esmError, filepath);
    default:
      throw new Error("Internal Babel error: unreachable code.");
  }
}
function loadCtsDefault(filepath) {
  const ext = ".cts";
  const hasTsSupport = !!(require.extensions[".ts"] || require.extensions[".cts"] || require.extensions[".mts"]);
  let handler;
  if (!hasTsSupport) {
    const opts = {
      babelrc: false,
      configFile: false,
      sourceType: "unambiguous",
      sourceMaps: "inline",
      sourceFileName: _path().basename(filepath),
      presets: [[getTSPreset(filepath), Object.assign({
        onlyRemoveTypeImports: true,
        optimizeConstEnums: true
      }, {
        allowDeclareFields: true
      })]]
    };
    handler = function (m, filename) {
      if (handler && filename.endsWith(ext)) {
        try {
          return m._compile((0, _transformFile.transformFileSync)(filename, Object.assign({}, opts, {
            filename
          })).code, filename);
        } catch (error) {
          if (!hasTsSupport) {
            const packageJson = require("@babel/preset-typescript/package.json");
            if (_semver().lt(packageJson.version, "7.21.4")) {
              console.error("`.cts` configuration file failed to load, please try to update `@babel/preset-typescript`.");
            }
          }
          throw error;
        }
      }
      return require.extensions[".js"](m, filename);
    };
    require.extensions[ext] = handler;
  }
  try {
    return loadCjsDefault(filepath);
  } finally {
    if (!hasTsSupport) {
      if (require.extensions[ext] === handler) delete require.extensions[ext];
      handler = undefined;
    }
  }
}
function getTSPreset(filepath) {
  try {
    return require("@babel/preset-typescript");
  } catch (error) {
    if (error.code !== "MODULE_NOT_FOUND") throw error;
    let message = "You appear to be using a .cts file as Babel configuration, but the `@babel/preset-typescript` package was not found: please install it!";
    {
      if (process.versions.pnp) {
        message += `
If you are using Yarn Plug'n'Play, you may also need to add the following configuration to your .yarnrc.yml file:

packageExtensions:
\t"@babel/core@*":
\t\tpeerDependencies:
\t\t\t"@babel/preset-typescript": "*"
`;
      }
    }
    throw new _configError.default(message, filepath);
  }
}
0 && 0;

//# sourceMappingURL=module-types.js.map
