{
  name: 'Base Selector Tests',
  tests: [
    {
      name: 'base selector configuration',
      cases: [
        {
          name: 'supports disabling base selectors',
          test:
            local selector = (import '../selectors.libsonnet').new;
            selector(false).build(),
          expected: '',
        },
        {
          name: 'preserves order with base selectors disabled',
          test:
            local selector = (import '../selectors.libsonnet').new;
            selector(false)
            .label('z').eq('2')
            .label('a').eq('1')
            .build(),
          expected: 'a="1", z="2"',
        },
      ],
    },
  ],
}
