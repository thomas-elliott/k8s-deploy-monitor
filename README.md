# k8s-deploy-monitor
k8s deploy monitor is a Kubernetes controller that observes changes in Deployments, ReplicaSets, and Pods. When changes are detected, it sends a notification to a configurable webhook endpoint.

# Features
- Monitors changes in Kubernetes Deployments, ReplicaSets, and Pods.
- Notifies a configurable webhook endpoint about detected changes.
- Can include an optional API key for webhook authentication.

# Prerequisites
Go 1.26.10
Kubernetes cluster (for deployment)
kubectl and kubeconfig properly set up to communicate with your cluster.

### Running on the cluster
1. Build and push your image to the location specified by `IMG`:

```sh
make docker-build docker-push IMG=<some-registry>/k8s-deploy-monitor:tag
```

2. Deploy the controller to the cluster with the image specified by `IMG`:

```sh
make deploy IMG=<some-registry>/k8s-deploy-monitor:tag
```

3. Install Instances of Custom Resources:

```sh
kubectl apply -f config/samples/
```

# Monitor resource
A `Monitor` is cluster-scoped and sets which namespaces to watch and which endpoints to notify. See [config/samples/deploy_v1alpha1_monitor.yaml](config/samples/deploy_v1alpha1_monitor.yaml) for an example. When both `namespaceRegex` and `namespaceSelector` are set, a namespace must match both.

# Authenticating webhooks
If a Monitor has `auth` set, each webhook request includes an API key in the header named by `auth.header` (default `X-Api-Key`). The key is read from the Secret in `auth.secretRef`, which must be in the controller's namespace (`k8s-deploy-monitor-system` when deployed with `make deploy`):

```sh
kubectl create secret generic monitor-sample-api-key -n k8s-deploy-monitor-system --from-literal=apiKey=<your-api-key>
```

# Webhook payload
Example Deployment payload:

```json
{
  "deployment": "my-app",
  "revision": "3",
  "namespace": "prod-web",
  "images": [
    { "name": "app", "fullName": "registry.example.com/team/app:v2", "tag": "v2" }
  ],
  "readyReplicas": 2,
  "desiredReplicas": 2,
  "status": {
    "observedGeneration": 3,
    "replicas": 2,
    "updatedReplicas": 2,
    "readyReplicas": 2,
    "availableReplicas": 2
  },
  "labels": { "app": "my-app" }
}
```

ReplicaSet and Pod payloads follow the same pattern; see [internal/controller/payloads.go](internal/controller/payloads.go). When a resource is deleted, `{"deployment": "my-app", "namespace": "prod-web", "deleted": true}` is sent.

# Status
`kubectl get monitors` shows two conditions: `Ready` (the Monitor's configuration and API key Secret are valid) and `Delivering` (the last webhook call succeeded).

### Uninstall CRDs
To delete the CRDs from the cluster:

```sh
make uninstall
```

### Undeploy controller
Undeploy the controller from the cluster:

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
If you are editing the API definitions, generate the manifests such as CRDs and RBAC using:

```sh
make manifests generate
```

**NOTE:** Run `make --help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

MIT, see [LICENSE](LICENSE).