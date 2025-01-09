/*
* Handles simple color darkening and lightening
*/

local clean(color) =
  std.strReplace(std.strReplace(std.strReplace(std.strReplace(color, 'super-', ''), 'semi-', ''), 'light-', ''), 'dark-', '');

local isHex(color) =
  std.startsWith(color, '#');

local convert(color, shade) =
  if (isHex(color)) then
    color
  else if shade == '' then
    clean(color)
  else
    '%s-%s' % [shade, clean(color)];

{
  lighter(color)::
    convert(color, 'super-light'),
  light(color)::
    convert(color, 'light'),
  normal(color)::
    convert(color, ''),
  dark(color)::
    convert(color, 'semi-dark'),
  darker(color)::
    convert(color, 'dark'),
}
