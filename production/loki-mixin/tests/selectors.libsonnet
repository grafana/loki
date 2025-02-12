{
  name: 'Selector Chain Tests',
  tests: [
    {
      name: 'selector method chaining',
      cases: [
        {
          name: 'supports multiple label operations in any order',
          test:
            local selector = (import '../selectors.libsonnet').new;
            selector()
            .label('a').eq('1')
            .label('b').neq('2')
            .label('c').re('3|4')
            .label('d').nre('5|6')
            .build(),
          expected: 'a="1", b!="2", c=~"3|4", cluster="$cluster", d!~"5|6", namespace="$namespace"',
        },
        {
          name: 'handles duplicate label operations',
          test:
            local selector = (import '../selectors.libsonnet').new;
            selector()
            .label('test').eq('1')
            .label('test').eq('2')
            .build(),
          expected: 'cluster="$cluster", namespace="$namespace", test="2"',
        },
      ],
    },
  ],
}
