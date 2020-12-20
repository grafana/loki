# kuberesolver
Grpc Client-Side Load Balancer with Kubernetes name resolver

```go
// Register kuberesolver to grpc
kuberesolver.RegisterInCluster()
// is same as
resolver.Register(kuberesolver.NewBuilder(nil))
// you can bring your own k8s client, below is default behaviour
client, err := kuberesolver.NewInClusterK8sClient()
resolver.Register(kuberesolver.NewBuilder(client))

// USAGE:
// if schema is 'kubernetes' then grpc will use kuberesolver to resolve addresses
cc, err := grpc.Dial("kubernetes:///service-name.namespace:portname", opts...)
```

An url can be one of the following, [grpc naming docs](https://github.com/grpc/grpc/blob/master/doc/naming.md)
```
kubernetes:///service-name:8080
kubernetes:///service-name:portname
kubernetes:///service-name.namespace:8080

kubernetes://namespace/service-name:8080
kubernetes://service-name:8080/
kubernetes://service-name.namespace:8080/

```
