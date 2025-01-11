{
  // these are wrapper functions to make it easier to create panels with common units
  short(params)::
    self.new(params + { unit: 'short' }),

  percent(params)::
    self.new(params + { unit: 'percent' }),

  currency(params)::
    self.new(params + { unit: 'currencyUSD' }),

  bytes(params)::
    self.new(params + { unit: 'bytes' }),

  bytesRate(params)::
    self.new(params + { unit: 'binBps' }),

  gbytes(params)::
    self.new(params + { unit: 'gbytes' }),

  // count per second
  cps(params)::
    self.new(params + { unit: 'cps' }),

  // requests per second
  reqps(params)::
    self.new(params + { unit: 'reqps' }),

  // queries per second
  qps(params)::
    self.reqps(params),

  // seconds
  s(params)::
    self.new(params + { unit: 's' }),

  seconds(params)::
    self.s(params),

  // milliseconds
  ms(params)::
    self.new(params + { unit: 'ms' }),

  milliseconds(params)::
    self.ms(params),
}
