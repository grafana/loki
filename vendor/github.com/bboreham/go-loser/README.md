# go-loser
Loser Tree data structure, for fast k-way merge

I will be speaking about this at [GopherCon](https://www.gophercon.com/agenda/session/1160355) on 27th Sept 2023.

There are currently two versions of the code on two Git branches: [main](https://github.com/bboreham/go-loser/tree/main), which works for built-in types like `int` and `string`, and [any](https://github.com/bboreham/go-loser/tree/any) which works on any type but requires you to pass in a function pointer to do `less` comparisons.

See https://en.wikipedia.org/wiki/K-way_merge_algorithm#Tournament_Tree for more details on the algorithm.
