package logql

// avg(x) -> sum(x)/count(x)

// sum(x) -> sum(sum(x, shard=1) ++ sum(x, shard=2)...)

// max(x) -> max(max(x, shard=1) ++ max(x, shard=2)...)

// min(x) -> min(min(x, shard=1) ++ min(x, shard=2)...)

// count(x) -> sum(count(x, shard=1) ++ count(x, shard=2)...)

// topk(x) -> topk(topk(x, shard=1) ++ topk(x, shard=2)...)

// botk(x) -> botk(botk(x, shard=1) ++ botk(x, shard=2)...)

// rate(x) -> rate(x, shard=1) ++ rate(x, shard=2)...

// count_over_time(x) -> count_over_time(x, shard=1) ++ count_over_time(x, shard=2)...
