# kuberesolver
Grpc Client-Side Load Balancer with Kubernetes name resolver

```go
//New balancer for default namespace
balancer := kuberesolver.New() 
//Dials with RoundRobin lb and kubernetes name resolver. if url schema is not 'kubernetes' than uses dns
cc, err := balancer.Dial("kubernetes://service-name:portname", opts...) 
// or, add balancer as dial option bu this does not fallback to dns if schema is not 'kubernetes'
cc, err := grpc.Dial("kubernetes://service-name:portname", balancer.DialOption(), opts...)
```
An url can be one of the following
```
kubernetes://service-name:portname    uses kubernetes api to fetch endpoints and port names
kubernetes://service-name:8080        uses kubernetes api to fetch endpoints but uses given port
dns://service-name:8080               does not use lb
service-name:8080
```
