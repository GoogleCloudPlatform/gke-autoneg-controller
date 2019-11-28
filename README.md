# Autoneg GKE controller

`autoneg` provides simple custom integration between GKE and GCLB. `autoneg` is a GKE controller which works in conjunction with the GKE NEG controller to manage integration between your GKE service endpoints and GCLB backend services.

GKE users may wish to register NEG backends from multiple clusters into the same backend service, or may wish to orchestrate advanced deployment strategies in a custom fashion. `autoneg` can enable those use cases.

## How it works

`autoneg` depends on the GKE NEG controller to manage the lifecycle of NEGs corresponding to your GKE services. `autoneg` will associate those NEGs as backends to the GCLB backend service named in the `autoneg` configuration.

Since `autoneg` depends explicitly on the GKE NEG controller, it also inherits the same scope. `autoneg` only takes action based on a GKE service, and does not make any changes corresponding to pods or deployments. Only changes to the service will cause any action by `autoneg`.

On deleting the GKE service, `autoneg` will deregister NEGs from the specified backend service, and the GKE NEG controller will then delete the NEGs.

## Using Autoneg

In your GKE service, two annotations are required in your service definition:

* `cloud.google.com/neg` enables the GKE NEG controller; specify as [standalone NEGs](https://cloud.google.com/kubernetes-engine/docs/how-to/standalone-neg)
* `anthos.cft.dev/autoneg` specifies name and other configuration

```yaml
metadata:
  annotations:
    cloud.google.com/neg: '{"exposed_ports": {"80":{}}}'
    anthos.cft.dev/autoneg: '{"name":"autoneg_test", "max_rate_per_endpoint":1000}'
```

`autoneg` will detect the NEGs that are created by the GKE NEG controller, and register them with the backend service specified in the `autoneg` configuration annotation.

Only the NEGs created by the GKE NEG controller will be added or removed from your backend service. This mechanism should be safe to use across multiple clusters.

Note: `autoneg` will initialize the `capacityScaler` variable to 1 on new registrations. On any changes, `autoneg` will leave whatever is set in that value. The `capacityScaler` mechanism can be used orthogonally by interactive tooling to manage traffic shifting in such uses cases as deployment or failover.

## Autoneg Configuration

Specify options to configure the backends representing the NEGs that will be associated with the backend service. Options can be referenced in the `backends` section of the [REST resource definition](https://cloud.google.com/compute/docs/reference/rest/v1/backendServices). Only options listed here are available in `autoneg`.

### Options

* `name`: optional. The name of the backend service to register backends with. Defaults to GKE service name.
* `max_rate_per_endpoint`: required. Integer representing the maximum rate a pod can handle.

## Installation

`autoneg` is based on [Kubebuilder](https://kubebuilder.io), and as such, you can customize and deploy `autoneg` according to the Kubebuilder "Run It On the Cluster" section of the [Quick Start](https://kubebuilder.io/quick-start.html#run-it-on-the-cluster). `autoneg` does not define a CRD, so you can skip any Kubebuilder steps involving CRDs.

For your convenience, you can also use the default output of Kubebuilder's `make deploy` step along with a public image. Simply `kubectl apply -f deploy/autoneg.yaml` to create the `autoneg-system` namespace and deploy `autoneg` into it.

## IAM considerations

As `autoneg` is accessing GCP APIs, you must ensure that the controller has authorization to call those APIs. You may consider using Workload Identity to specify a GCP service account that `autoneg` operates under.
