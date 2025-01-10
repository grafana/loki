local config = import '../../config.libsonnet';

{
  methodNameFromKey(key)::
    local methodSuffix = std.asciiUpper(std.substr(key, 0, 1)) + std.substr(key, 1, std.length(key) - 1);
    'with' + methodSuffix,

  applyOptions(optionsObject, keys, params)::
    std.foldl(
      function(acc, key)
        local methodName = self.methodNameFromKey(key);
        if std.objectHasAll(params, key) && params[key] != null && std.objectHasAll(optionsObject, methodName) then
          acc + optionsObject[methodName](params[key])
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
