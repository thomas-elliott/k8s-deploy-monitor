# k8s-deploy-monitor
k8s deploy monitor is a Kubernetes controller that observes changes in Deployments, ReplicaSets, and Pods. When changes are detected, it sends a notification to a configurable webhook endpoint.

# Features
- Monitors changes in Kubernetes Deployments, ReplicaSets, and Pods.
- Notifies a configurable webhook endpoint about detected changes.
- Can include an optional API key for webhook authentication.

# Prerequisites
Go 1.19
Kubernetes cluster (for deployment)
kubectl and kubeconfig properly set up to communicate with your cluster.
Operator SDK

```sh
kubectl apply -f config/samples/
```

2. Build and push your image to the location specified by `IMG`:

```sh
make docker-build docker-push IMG=<some-registry>/k8s-deploy-monitor:tag
```

3. Deploy the controller to the cluster with the image specified by `IMG`:

```sh
make deploy IMG=<some-registry>/k8s-deploy-monitor:tag
```

### Uninstall CRDs
To delete the CRDs from the cluster:

```sh
make uninstall
```

### Undeploy controller
UnDeploy the controller from the cluster:

```sh
make undeploy
```

## Contributing
Pull requests and issues welcome.

### How it works
This project aims to follow the Kubernetes [Operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/).

It uses [Controllers](https://kubernetes.io/docs/concepts/architecture/controller/),
which provide a reconcile function responsible for synchronizing resources until the desired state is reached on the cluster.

### Test It Out
1. Install the CRDs into the cluster:

```sh
make install
```

2. Run your controller (this will run in the foreground, so switch to a new terminal if you want to leave it running):

```sh
make run
```

**NOTE:** You can also run this in one step by running: `make install run`

### Modifying the API definitions
If you are editing the API definitions, generate the manifests such as CRs or CRDs using:

```sh
make manifests
```

**NOTE:** Run `make --help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

TODO