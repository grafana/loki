local selectors = (import 'selectors.libsonnet').new;

[
  {
    name: 'it does not duplicate the cluster label',
    test: 'selectors().cluster().build()',
    selector: selectors().cluster().build(),
  },
  {
    name: 'it does not duplicate the namespace label',
    test: 'selectors().namespace().build()',
    selector: selectors().namespace().build(),
  },
  {
    name: 'it builds a selector for a single job',
    test: 'selectors().job("ingester").build()',
    selector: selectors().job('ingester').build(),
  },
  {
    name: 'it builds a selector for multiple jobs',
    test: 'selectors().job(["ingester", "distributor"]).build()',
    selector: selectors().job(['ingester', 'distributor']).build(),
  },
  {
    name: 'it builds a selector that duplicates the job label',
    test: 'selectors().job(["ingester", "distributor", "distributor"]).build()',
    selector: selectors().job(['ingester', 'distributor', 'distributor']).build(),
  },
  {
    name: 'it builds a selector for a single pod',
    test: 'selectors().pod("ingester").build()',
    selector: selectors().pod('ingester').build(),
  },
  {
    name: 'it builds a selector for multiple pods',
    test: 'selectors().pod(["ingester", "distributor"]).build()',
    selector: selectors().pod(['ingester', 'distributor']).build(),
  },
  {
    name: 'it builds a selector for a single container',
    test: 'selectors().container("ingester").build()',
    selector: selectors().container('ingester').build(),
  },
  {
    name: 'it builds a selector for multiple containers',
    test: 'selectors().container(["ingester", "distributor"]).build()',
    selector: selectors().container(['ingester', 'distributor']).build(),
  },
    {
    name: 'it builds a job selector for the adminApi component using the wrapper',
    test: 'selectors().adminApi().build()',
    selector: selectors().adminApi().build(),
  },
  {
    name: 'it builds a pod selector for the adminApi component using the wrapper',
    test: 'selectors().adminApi(label="pod").build()',
    selector: selectors().adminApi(label='pod').build(),
  },
  {
    name: 'it builds a container selector for the adminApi component using the wrapper',
    test: 'selectors().adminApi(label="container").build()',
    selector: selectors().adminApi(label='container').build(),
  },

  {
    name: 'it builds a job selector for the bloomBuilder component using the wrapper',
    test: 'selectors().bloomBuilder().build()',
    selector: selectors().bloomBuilder().build(),
  },
  {
    name: 'it builds a pod selector for the bloomBuilder component using the wrapper',
    test: 'selectors().bloomBuilder(label="pod").build()',
    selector: selectors().bloomBuilder(label='pod').build(),
  },
  {
    name: 'it builds a container selector for the bloomBuilder component using the wrapper',
    test: 'selectors().bloomBuilder(label="container").build()',
    selector: selectors().bloomBuilder(label='container').build(),
  },

  {
    name: 'it builds a job selector for the bloomGateway component using the wrapper',
    test: 'selectors().bloomGateway().build()',
    selector: selectors().bloomGateway().build(),
  },
  {
    name: 'it builds a pod selector for the bloomGateway component using the wrapper',
    test: 'selectors().bloomGateway(label="pod").build()',
    selector: selectors().bloomGateway(label='pod').build(),
  },
  {
    name: 'it builds a container selector for the bloomGateway component using the wrapper',
    test: 'selectors().bloomGateway(label="container").build()',
    selector: selectors().bloomGateway(label='container').build(),
  },

  {
    name: 'it builds a job selector for the bloomPlanner component using the wrapper',
    test: 'selectors().bloomPlanner().build()',
    selector: selectors().bloomPlanner().build(),
  },
  {
    name: 'it builds a pod selector for the bloomPlanner component using the wrapper',
    test: 'selectors().bloomPlanner(label="pod").build()',
    selector: selectors().bloomPlanner(label='pod').build(),
  },
  {
    name: 'it builds a container selector for the bloomPlanner component using the wrapper',
    test: 'selectors().bloomPlanner(label="container").build()',
    selector: selectors().bloomPlanner(label='container').build(),
  },

  {
    name: 'it builds a job selector for the compactor component using the wrapper',
    test: 'selectors().compactor().build()',
    selector: selectors().compactor().build(),
  },
  {
    name: 'it builds a pod selector for the compactor component using the wrapper',
    test: 'selectors().compactor(label="pod").build()',
    selector: selectors().compactor(label='pod').build(),
  },
  {
    name: 'it builds a container selector for the compactor component using the wrapper',
    test: 'selectors().compactor(label="container").build()',
    selector: selectors().compactor(label='container').build(),
  },

  {
    name: 'it builds a job selector for the cortexGateway component using the wrapper',
    test: 'selectors().cortexGateway().build()',
    selector: selectors().cortexGateway().build(),
  },
  {
    name: 'it builds a pod selector for the cortexGateway component using the wrapper',
    test: 'selectors().cortexGateway(label="pod").build()',
    selector: selectors().cortexGateway(label='pod').build(),
  },
  {
    name: 'it builds a container selector for the cortexGateway component using the wrapper',
    test: 'selectors().cortexGateway(label="container").build()',
    selector: selectors().cortexGateway(label='container').build(),
  },

  {
    name: 'it builds a job selector for the distributor component using the wrapper',
    test: 'selectors().distributor().build()',
    selector: selectors().distributor().build(),
  },
  {
    name: 'it builds a pod selector for the distributor component using the wrapper',
    test: 'selectors().distributor(label="pod").build()',
    selector: selectors().distributor(label='pod').build(),
  },
  {
    name: 'it builds a container selector for the distributor component using the wrapper',
    test: 'selectors().distributor(label="container").build()',
    selector: selectors().distributor(label='container').build(),
  },

  {
    name: 'it builds a job selector for the gateway component using the wrapper',
    test: 'selectors().gateway().build()',
    selector: selectors().gateway().build(),
  },
  {
    name: 'it builds a pod selector for the gateway component using the wrapper',
    test: 'selectors().gateway(label="pod").build()',
    selector: selectors().gateway(label='pod').build(),
  },
  {
    name: 'it builds a container selector for the gateway component using the wrapper',
    test: 'selectors().gateway(label="container").build()',
    selector: selectors().gateway(label='container').build(),
  },

  {
    name: 'it builds a job selector for the indexGateway component using the wrapper',
    test: 'selectors().indexGateway().build()',
    selector: selectors().indexGateway().build(),
  },
  {
    name: 'it builds a pod selector for the indexGateway component using the wrapper',
    test: 'selectors().indexGateway(label="pod").build()',
    selector: selectors().indexGateway(label='pod').build(),
  },
  {
    name: 'it builds a container selector for the indexGateway component using the wrapper',
    test: 'selectors().indexGateway(label="container").build()',
    selector: selectors().indexGateway(label='container').build(),
  },

  {
    name: 'it builds a job selector for the ingester component using the wrapper',
    test: 'selectors().ingester().build()',
    selector: selectors().ingester().build(),
  },
  {
    name: 'it builds a pod selector for the ingester component using the wrapper',
    test: 'selectors().ingester(label="pod").build()',
    selector: selectors().ingester(label='pod').build(),
  },
  {
    name: 'it builds a container selector for the ingester component using the wrapper',
    test: 'selectors().ingester(label="container").build()',
    selector: selectors().ingester(label='container').build(),
  },

  {
    name: 'it builds a job selector for the overridesExporter component using the wrapper',
    test: 'selectors().overridesExporter().build()',
    selector: selectors().overridesExporter().build(),
  },
  {
    name: 'it builds a pod selector for the overridesExporter component using the wrapper',
    test: 'selectors().overridesExporter(label="pod").build()',
    selector: selectors().overridesExporter(label='pod').build(),
  },
  {
    name: 'it builds a container selector for the overridesExporter component using the wrapper',
    test: 'selectors().overridesExporter(label="container").build()',
    selector: selectors().overridesExporter(label='container').build(),
  },

  {
    name: 'it builds a job selector for the patternIngester component using the wrapper',
    test: 'selectors().patternIngester().build()',
    selector: selectors().patternIngester().build(),
  },
  {
    name: 'it builds a pod selector for the patternIngester component using the wrapper',
    test: 'selectors().patternIngester(label="pod").build()',
    selector: selectors().patternIngester(label='pod').build(),
  },
  {
    name: 'it builds a container selector for the patternIngester component using the wrapper',
    test: 'selectors().patternIngester(label="container").build()',
    selector: selectors().patternIngester(label='container').build(),
  },
  {
    name: 'it builds a job selector for the querier component using the wrapper',
    test: 'selectors().querier().build()',
    selector: selectors().querier().build(),
  },
  {
    name: 'it builds a pod selector for the querier component using the wrapper',
    test: 'selectors().querier(label="pod").build()',
    selector: selectors().querier(label='pod').build(),
  },
  {
    name: 'it builds a container selector for the querier component using the wrapper',
    test: 'selectors().querier(label="container").build()',
    selector: selectors().querier(label='container').build(),
  },

  {
    name: 'it builds a job selector for the queryFrontend component using the wrapper',
    test: 'selectors().queryFrontend().build()',
    selector: selectors().queryFrontend().build(),
  },
  {
    name: 'it builds a pod selector for the queryFrontend component using the wrapper',
    test: 'selectors().queryFrontend(label="pod").build()',
    selector: selectors().queryFrontend(label='pod').build(),
  },
  {
    name: 'it builds a container selector for the queryFrontend component using the wrapper',
    test: 'selectors().queryFrontend(label="container").build()',
    selector: selectors().queryFrontend(label='container').build(),
  },

  {
    name: 'it builds a job selector for the queryScheduler component using the wrapper',
    test: 'selectors().queryScheduler().build()',
    selector: selectors().queryScheduler().build(),
  },
  {
    name: 'it builds a pod selector for the queryScheduler component using the wrapper',
    test: 'selectors().queryScheduler(label="pod").build()',
    selector: selectors().queryScheduler(label='pod').build(),
  },
  {
    name: 'it builds a container selector for the queryScheduler component using the wrapper',
    test: 'selectors().queryScheduler(label="container").build()',
    selector: selectors().queryScheduler(label='container').build(),
  },

  {
    name: 'it builds a job selector for the ruler component using the wrapper',
    test: 'selectors().ruler().build()',
    selector: selectors().ruler().build(),
  },
  {
    name: 'it builds a pod selector for the ruler component using the wrapper',
    test: 'selectors().ruler(label="pod").build()',
    selector: selectors().ruler(label='pod').build(),
  },
  {
    name: 'it builds a container selector for the ruler component using the wrapper',
    test: 'selectors().ruler(label="container").build()',
    selector: selectors().ruler(label='container').build(),
  },
  {
    name: 'it builds a selector for a single label',
    test: 'selectors().label("route").re("v1|v2|v3").build()',
    selector: selectors().label('route').re('v1|v2|v3').build(),
  },
  {
    name: 'it builds a selector for a single label with a tenant',
    test: 'selectors().withLabel("tenant", "=", "$tenant").build()',
    selector: selectors().withLabel('tenant', '=', '$tenant').build(),
  },
  {
    name: 'it builds a selector for a single label with a user',
    test: 'selectors().withLabel("user", "=", "$user").build()',
    selector: selectors().withLabel('user', '=', '$user').build(),
  },
  {
    name: 'it builds a selector for a multiple components from the shorthand wrapper methods',
    test: 'selectors().querier().queryFrontend().queryScheduler().build()',
    selector: selectors().querier().queryFrontend().queryScheduler().build(),
  },
  {
    name: 'it builds a selector for a multiple components from the shorthand wrapper methods',
    test: 'selectors().job(["querier", "query-frontend", "query-scheduler"]).build()',
    selector: selectors().job(['querier', 'query-frontend', 'query-scheduler']).build(),
  },
]
