package pipeline

// a CacheCondition should return true if the cacher should cache the step.
// In other words, returning false means that the step must run.
type CacheCondition func() bool

// A Cacher defines behavior for caching data.
// Some behaviors that can happen when dealing with a cacheable step:
// * The provided Step, using an expensive process, produces some consistent output, possibly on the filesystem. If nothing changes in between runs, then we can re-use the output in the current step and skip this one.
//   * The most common example of this is `npm install` producing the `node_modules` folder, which can be re-used if `package-lock.json` is unchanged.
// * The provided Step, using an expensive process, calculates a value. If nothing changes in between runs, then this value can be reused.
type Cacher func(Step)
