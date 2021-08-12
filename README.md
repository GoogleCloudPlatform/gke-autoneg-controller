# Autoneg GKE controller

`autoneg` provides simple custom integration between GKE and GCLB (both external and internal).  `autoneg` is a GKE controller which works in conjunction with the GKE NEG controller to manage integration between your GKE service endpoints and GCLB backend services.

GKE users may wish to register NEG backends from multiple clusters into the same backend service, or may wish to orchestrate advanced deployment strategies in a custom fashion, or offer the same service via protected public endpoint and more lax internal endpoint. `autoneg` can enable those use cases.

## How it works

`autoneg` depends on the GKE NEG controller to manage the lifecycle of NEGs corresponding to your GKE services. `autoneg` will associate those NEGs as backends to the GCLB backend service named in the `autoneg` configuration.

Since `autoneg` depends explicitly on the GKE NEG controller, it also inherits the same scope. `autoneg` only takes action based on a GKE service, and does not make any changes corresponding to pods or deployments. Only changes to the service will cause any action by `autoneg`.

On deleting the GKE service, `autoneg` will deregister NEGs from the specified backend service, and the GKE NEG controller will then delete the NEGs.

## Using Autoneg

In your GKE service, two annotations are required in your service definition:

* `cloud.google.com/neg` enables the GKE NEG controller; specify as [standalone NEGs](https://cloud.google.com/kubernetes-engine/docs/how-to/standalone-neg)
* `controller.autoneg.dev/neg` specifies name and other configuration
   * Previous version used `anthos.cft.dev/autoneg` as annotation and it's still supported, but deprecated and will be removed in subsequent releases.
   * Note that `name` is optional here and defaults to a value generated following negNameTemplage. The template defaults to `{name}-{port}` and can be configured using `--neg-name-template` flag. It can contain `namespace`, `name`, `port` and `hash` and the non hash values will be truncated evenly if the full name is longer than 63 characters. `<hash>` is generated using full length `namespace`, `name` and `port` to avoid name collisions when truncated.
```yaml
metadata:
  annotations:
    cloud.google.com/neg: '{"exposed_ports": {"80":{},"443":{}}}'
    controller.autoneg.dev/neg: '{"backend_services":{"80":[{"name":"http-be","max_rate_per_endpoint":100}],"443":[{"name":"https-be","max_connections_per_endpoint":1000}]}}
    # For L7 ILB (regional) backends 
    # controller.autoneg.dev/neg: '{"backend_services":{"80":[{"name":"http-be","region":"europe-west4","max_rate_per_endpoint":100}],"443":[{"name":"https-be","region":"europe-west4","max_connections_per_endpoint":1000}]}}
```

`autoneg` will detect the NEGs that are created by the GKE NEG controller, and register them with the backend service specified in the `autoneg` configuration annotation.

Only the NEGs created by the GKE NEG controller will be added or removed from your backend service. This mechanism should be safe to use across multiple clusters.

Note: `autoneg` will initialize the `capacityScaler` variable to 1 on new registrations. On any changes, `autoneg` will leave whatever is set in that value. The `capacityScaler` mechanism can be used orthogonally by interactive tooling to manage traffic shifting in such uses cases as deployment or failover.

## Autoneg Configuration

Specify options to configure the backends representing the NEGs that will be associated with the backend service. Options can be referenced in the `backends` section of the [REST resource definition](https://cloud.google.com/compute/docs/reference/rest/v1/backendServices). Only options listed here are available in `autoneg`.

### Options

* `name`: optional. The name of the backend service to register backends with. Defaults to a value generated using the given template.
   * The default name value for old `anthos.cft.dev/autoneg` annotation is service name.
* `region`: optional. Used to specify that this is a regional backend service.
* `max_rate_per_endpoint`: required/optional. Integer representing the maximum rate a pod can handle. Pick either rate or connection.
* `max_connections_per_endpoint`: required/optional. Integer representing the maximum amount of connections a pod can handle. Pick either rate or connection.

## IAM considerations

As `autoneg` is accessing GCP APIs, you must ensure that the controller has authorization to call those APIs. To follow the principle of least privilege, it is recommended that you configure your cluster with [Workload Identity](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity) to limit permissions to a GCP service account that `autoneg` operates under. If you choose not to use [Workload Identity](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity), you will need to create your GKE cluster with the "cloud-platform" scope.

## Installation

First, set up the GCP resources necessary to support [Workload Identity](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity), run the script:
```
PROJECT_ID=myproject deploy/workload_identity.sh
```
If you are using Shared VPC, ensure that the `autoneg-system` service account has the `compute.networkUser` role in the Shared VPC host project:
```
gcloud projects add-iam-policy-binding \
  --role roles/compute.networkUser \
  --member "serviceAccount:autoneg-system@${PROJECT_ID}.iam.gserviceaccount.com" \
  ${HOST_PROJECT_ID}
```

Lastly, on each cluster in your project where you'd like to install `autoneg` (version `v0.9.3`), run these two commands:
```
kubectl apply -f deploy/autoneg.yaml

kubectl annotate sa -n autoneg-system default \
  iam.gke.io/gcp-service-account=autoneg-system@${PROJECT_ID}.iam.gserviceaccount.com
```
This will create all the Kubernetes resources required to support `autoneg` and annotate the default service account in the `autoneg-system` namespace to associate a GCP service account using [Workload Identity](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity). 

### Installation via Terraform

You can use the Terraform module in `terraform/autoneg` to deploy Autoneg in a GKE cluster of your choice.

Example:

```tf
provider "google" {
}

provider "kubernetes" {
  cluster_ca_certificate = "..."
  host                   = "..."
  token                  = "..."
}

module "autoneg" {
  source = "github.com/GoogleCloudPlatform/gke-autoneg-controller//terraform/autoneg"

  project_id = "your-project-id"
}
```

### Customizing your installation

`autoneg` is based on [Kubebuilder](https://kubebuilder.io), and as such, you can customize and deploy `autoneg` according to the Kubebuilder "Run It On the Cluster" section of the [Quick Start](https://kubebuilder.io/quick-start.html#run-it-on-the-cluster). `autoneg` does not define a CRD, so you can skip any Kubebuilder steps involving CRDs.

The included `deploy/autoneg.yaml` is the default output of Kubebuilder's `make deploy` step, coupled with a public image.

Do keep in mind the additional configuration to enable [Workload Identity](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity).
