{
  name: 'Component Configuration Tests',
  tests: [
    {
      name: 'component config validation',
      cases: [
        {
          name: 'handles missing component config',
          test:
            local selector = (import '../selectors.libsonnet').new;
            selector().job('non-existent-component').build(),
          expected: 'cluster="$cluster", job=~"($namespace)/(non-existent-component)", namespace="$namespace"',
        },
      ],
    },
  ],
}
