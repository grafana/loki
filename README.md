

`docker build -t loki-canary:latest .`

`kubectl run loki-canary --generator=run-pod/v1 --image=loki-canary:latest --restart=Never --image-pull-policy=Never  --labels=name=loki-canary`