local config = import '../../config.libsonnet';

{
  debug(obj)::
    std.trace(std.toString(obj), obj),

  toCamelCase(str)::
    local parts = std.split(str, '-');
    parts[0] + std.join('', [
      std.asciiUpper(std.substr(part, 0, 1)) +
      std.substr(part, 1, std.length(part) - 1)
      for part in parts[1:]
    ]),

  fromCamelCase(str)::
    local chars = std.stringChars(str);
    local result = std.foldl(
      function(acc, char)
        if std.asciiUpper(char) == char then
          acc + '-' + std.asciiLower(char)
        else
          acc + char,
      chars,
      ''
    );
    // Remove leading dash if present
    if std.startsWith(result, '-') then
      std.substr(result, 1, std.length(result) - 1)
    else
      result,

  methodNameFromKey(key)::
    local methodSuffix = std.asciiUpper(std.substr(key, 0, 1)) + std.substr(key, 1, std.length(key) - 1);
    'with' + methodSuffix,

  toMethodName(str)::
    'with' + std.asciiUpper(std.substr(str, 0, 1)) + std.substr(str, 1, std.length(str) - 1),

  keyNamesFromMethods(obj, exclude = [])::
    local methodNames = std.objectFields(obj);
    local excludeMethods = [self.toMethodName(ex) for ex in exclude];
    [
      local withoutWith = if std.startsWith(method, 'with') then
        std.substr(method, 4, std.length(method) - 4)
      else
        method;
      std.asciiLower(std.substr(withoutWith, 0, 1)) +
      std.substr(withoutWith, 1, std.length(withoutWith) - 1)
      for method in methodNames
      if method != 'new' && !std.member(excludeMethods, method)
    ],

  applyOptions(obj, keys, params)::
    std.foldl(
      function(acc, key)
        local methodName = self.methodNameFromKey(key);
        if std.objectHasAll(params, key) && params[key] != null && std.objectHasAll(obj, methodName) then
          acc + obj[methodName](params[key])
        else
          acc,
      keys,
      {}
    ),

  required(obj, keys, msg = 'Required parameter(s) missing: %s')::
    local result = [
      key
      for key in keys
      if !std.objectHas(obj, key) || obj[key] == null
    ];
    // if there are missing keys, throw an error
    if std.length(result) > 0 then
      // check to see if the message contains any %s, if so replace them with the keys,
      // otherwise just output the message
      if std.findSubstr('%s', msg) != -1 then
        error msg % std.join(', ', result)
      else
        error msg
    else
      true,
}
