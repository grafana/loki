function(k8sVersion)
  (import '../jsonnetfile.json') + {
    dependencies+: [{
      source: {
        git: {
          remote: 'https://github.com/jsonnet-libs/k8s-libsonnet.git',
          subdir: k8sVersion,
        },
      },
      version: 'main',
    }],
  }
