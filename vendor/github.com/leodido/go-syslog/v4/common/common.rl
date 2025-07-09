%%{
machine common;

# whitespace
sp = ' ';

# closing square bracket
csb = ']';

# double quote
dq = '"';

# backslash
bs = 0x5C;

# ", ], \
toescape = (dq | csb | bs);

# 1..9
nonzerodigit = '1'..'9';

# 0..59
sexagesimal = '0'..'5' . '0'..'9';

# 01..31
datemday_2digit = ('0' . nonzerodigit | '1'..'2' . '0'..'9' | '3' . '0'..'1');

#  1 ..  9, 10..31
datemday = (sp . nonzerodigit | '1'..'2' . '0'..'9' | '3' . '0'..'1');

# 01..12
datemonth = ('0' . nonzerodigit | '1' . '0'..'2');

datemmm = ('Jan' | 'Feb' | 'Mar' | 'Apr' | 'May' | 'Jun' | 'Jul' | 'Aug' | 'Sep' | 'Oct' | 'Nov' | 'Dec');

datefullyear = digit{4};

fulldate = datefullyear '-' datemonth '-' datemday_2digit;

# 01..23
timehour = ('0'..'1' . '0'..'9' | '2' . '0'..'3');

timeminute = sexagesimal;

timesecond = sexagesimal;

timesecfrac = '.' digit{1,6};

timenumoffset = ('+' | '-') timehour ':' timeminute;

timeoffset = 'Z' | timenumoffset;

hhmmss = timehour ':' timeminute ':' timesecond;

partialtime = hhmmss . timesecfrac?;

fulltime = partialtime . timeoffset;

# 1..191
privalrange = (('1' ('9' ('0'..'1'){,1} | '0'..'8' ('0'..'9'){,1}){,1}) | ('2'..'9' ('0'..'9'){,1}));

# 1..191 or 0
prival = (privalrange | '0');

hostnamerange = graph{1,255};

appnamerange = graph{1,48};

procidrange = graph{1,128};

msgidrange = graph{1,32};

sdname = (graph - ('=' | sp | csb | dq)){1,32};

# rfc 3629
utf8tail = 0x80..0xBF;

utf81 = 0x00..0x7F;

utf82 = 0xC2..0xDF utf8tail;

utf83 = 0xE0 0xA0..0xBF utf8tail | 0xE1..0xEC utf8tail{2} | 0xED 0x80..0x9F utf8tail | 0xEE..0xEF utf8tail{2};

utf84 = 0xF0 0x90..0xBF utf8tail{2} | 0xF1..0xF3 utf8tail{3} | 0xF4 0x80..0x8F utf8tail{2};

utf8char = utf81 | utf82 | utf83 | utf84;

utf8octets = utf8char*;

bom = 0xEF 0xBB 0xBF;

# utf8char except ", ], \
utf8charwodelims = utf8char - toescape;

}%%