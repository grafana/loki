# Chunk format

```
  |                 |             |
  | MagicNumber(4b) | version(1b) |
  |                 |             |
  --------------------------------------------------
  |         block-1 bytes         |  checksum (4b) |
  --------------------------------------------------
  |         block-2 bytes         |  checksum (4b) |
  --------------------------------------------------
  |         block-n bytes         |  checksum (4b) |
  --------------------------------------------------
  |         #blocks (uvarint)                      |
  --------------------------------------------------
  | #entries(uvarint) | mint, maxt (varint) | offset, len (uvarint) | uncompressedSize (uvarint) |
  ------------------------------------------------------------------------------------------------
  | #entries(uvarint) | mint, maxt (varint) | offset, len (uvarint) | uncompressedSize (uvarint) |
  ------------------------------------------------------------------------------------------------
  | #entries(uvarint) | mint, maxt (varint) | offset, len (uvarint) | uncompressedSize (uvarint) |
  ------------------------------------------------------------------------------------------------
  | #entries(uvarint) | mint, maxt (varint) | offset, len (uvarint) | uncompressedSize (uvarint) |
  ------------------------------------------------------------------------------------------------
  |                      checksum(from #blocks)                     |
  -------------------------------------------------------------------
  | metasOffset - offset to the point with #blocks |
  --------------------------------------------------
```
