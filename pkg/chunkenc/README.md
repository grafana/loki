# Chunk format


  |                 |             |
  | MagicNumber(4b) | version(1b) |
  |                 |             |
  ---------------------------------
  |         block-1 bytes         |
  ---------------------------------
  |         block-2 bytes         |
  ---------------------------------
  |         block-n bytes         |
  ---------------------------------
  |         #blocks (uvarint)     |
  ---------------------------------
  | #entries(uvarint) | mint, maxt (varint) | offset, len (uvarint) |
  -------------------------------------------------------------------
  | #entries(uvarint) | mint, maxt (varint) | offset, len (uvarint) |
  -------------------------------------------------------------------
  | #entries(uvarint) | mint, maxt (varint) | offset, len (uvarint) |
  -------------------------------------------------------------------
  | #entries(uvarint) | mint, maxt (varint) | offset, len (uvarint) |
  -------------------------------------------------------------------
  | metasOffset - offset to the point with #blocks |
  --------------------------------------------------
