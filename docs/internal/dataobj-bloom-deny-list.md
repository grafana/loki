# Bloom deny list

The bloom deny list names the structured metadata columns that get no bloom filter in the index.
The index builder and the dataobj-compactor read it from the per-tenant limits.
This document describes why the list exists, how a denied column is stored, and what it means for
queries and compaction.

## The problem

The index stores one bloom filter per column, per logs section of a data object.
The size of a filter grows with the number of distinct values that the section holds for the column.
A filter costs about 1.26 bytes for each estimated distinct value.

A column with a unique value on every line has as many distinct values as lines.
The OTLP push path adds such a column, `observed_timestamp`, to every line.
Its filters can be most of an index, but no query puts an equality matcher on it.
A bloom filter only helps an equality matcher, so these bytes never prune anything.

Dropping all high-cardinality columns is not an option.
Trace IDs and span IDs also have a unique value on most lines, and their filters prune sections.
So the list is manual: an operator names the columns to skip.

## The idea

A denied column still gets its bloom row, but the filter in the row matches every value.
A reader never prunes a section on a filter that matches every value, so no reader needs a change.
A reader that sees no row for a column assumes that the column does not exist, and prunes.
So the row must stay.

The index builder does not add the values of a denied column to a filter.
It also does not allocate the filter for the estimated number of values.

## The filter that matches every value

The filter has one bit and one hash function, and the bit is set.
Every hash maps to bit 0, so a lookup always returns true.
The marshaled filter is 32 bytes, and its size does not depend on the data.
It is a valid filter for the existing reader code.

## What a denied row holds

A denied row has the same columns as any bloom row.
The filter is the 32-byte filter above.
The builder still records the stream bitmap, the time range and the uncompressed size of the column.
No reader uses these values for a bloom row.
But the merge needs them: it rejects two rows of one section that have different time ranges.
So the rows of one section must stay equal whichever builder version wrote them.

## Compaction

A merge never combines two filters.
It copies the bloom rows of its inputs, one row for each (data object, section, column).
When a merge writes a row of a denied column, it replaces the filter with the filter that matches
every value.
So a merge shrinks the real filters that older indexes hold for a denied column.
A row of a column that is not denied keeps its filter.

Removing a column from the list does not bring the real filters back.
The list only applies to the indexes that a builder or a merge writes after the change.

## Configuration

The list is the `dataobj_bloom_deny_list` limit.
The default is `observed_timestamp`.
A tenant can override the default in the runtime config.
An empty value denies no column.

The index builder and the merge builder take the list of their tenant as an argument.
Their callers read it from the limits once for each builder.
A change applies to the indexes that are built after the change.
The dataobj builder and the compaction worker read the same limit, so both need the overrides module.
