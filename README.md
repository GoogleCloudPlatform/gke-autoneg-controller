# ![Autoneg GKE controller](assets/img/autoneg.png)

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0) ![Tests](https://github.com/GoogleCloudPlatform/gke-autoneg-controller/actions/workflows/tests.yml/badge.svg)

`autoneg` provides simple custom integration between GKE and Google Cloud Load Balancing (both external and internal). 
`autoneg` is a GKE-specific Kubernetes controller which works in conjunction with the GKE Network Endpoint Group (NEG) 
controller to manage integration between your Kubernetes service endpoints and GCLB backend services.

GKE users may wish to register NEG backends from multiple clusters into the same backend service, or may wish to 
orchestrate advanced deployment strategies in a custom or centralized fashion, or offer the same service via protected 
public endpoint and more lax internal endpoint. `autoneg` can enable those use cases.

## How it works

`autoneg` depends on the GKE NEG controller to manage the lifecycle of NEGs corresponding to your GKE services. 
`autoneg` will associate those NEGs as backends to the GCLB backend service named in the `autoneg` configuration.

Since `autoneg` depends explicitly on the [GKE NEG controller](https://cloud.google.com/kubernetes-engine/docs/how-to/standalone-neg), it 
also inherits the same scope. `autoneg` only takes action based on a [Kubernetes service](https://kubernetes.io/docs/concepts/services-networking/service/)
which has been annotated with `autoneg` configuration, and does not make any changes corresponding to pods or deployments. Only 
changes to the service will cause any action by `autoneg`.

On deleting the Service object, `autoneg` will deregister NEGs from the specified backend service, and the GKE 
NEG controller will then delete the NEGs.

## Using Autoneg

In your Kubernetes service, two annotations are required in your service definition:

* `cloud.google.com/neg` enables the GKE NEG controller; specify as [standalone NEGs](https://cloud.google.com/kubernetes-engine/docs/how-to/standalone-neg)
* `controller.autoneg.dev/neg` specifies name and other configuration
   * Previous version used `anthos.cft.dev/autoneg` as annotation and it's still supported, but deprecated and will be removed in subsequent releases.

### Example annotations
```yaml
metadata:
  annotations:
    cloud.google.com/neg: '{"exposed_ports": {"80":{},"443":{}}}'
    controller.autoneg.dev/neg: '{"backend_services":{"80":[{"name":"http-be","max_rate_per_endpoint":100}],"443":[{"name":"https-be","max_connections_per_endpoint":1000}]}}
    # For L7 ILB (regional) backends 
    # controller.autoneg.dev/neg: '{"backend_services":{"80":[{"name":"http-be","region":"europe-west4","max_rate_per_endpoint":100}],"443":[{"name":"https-be","region":"europe-west4","max_connections_per_endpoint":1000}]}}
```

Once configured, `autoneg` will detect the NEGs that are created by the GKE NEG controller, and register them with the backend 
service specified in the `autoneg` configuration annotation.

Only the NEGs created by the GKE NEG controller will be added or removed from your backend service. This mechanism should be safe to 
use across multiple clusters.

By default, `autoneg` will initialize the `capacityScaler` to 1, which means that the new backend will receive a proportional volume
of traffic according to the maximum rate or connections per endpoint configuration. You can customize this default by supplying
the `initial_capacity` variable, which may be useful to steer traffic in blue/green deployment scenarios. The `capacityScaler` mechanism can be used to manage traffic shifting in such uses cases as deployment or failover.

## Autoneg Configuration

Specify options to configure the backends representing the NEGs that will be associated with the backend service. Options can be referenced in the `backends` section of the [REST resource definition](https://cloud.google.com/compute/docs/reference/rest/v1/backendServices). Only options listed here are available in `autoneg`.

### Options

#### Autoneg annotation options

* `name`: optional. The name of the backend service to register backends with.
   * If `--enable-custom-service-names` flag (defaults to `true`) is set to `false`, the `name` values specified in new `autoneg` annotations 
     would be invalidated and fall back to the template generated names.
   * The default name value for old `anthos.cft.dev/autoneg` annotation is service name.
   * Note that `name` is optional here and defaults to a value generated following `default-backendservice-name`. 
* `region`: optional. Used to specify that this is a regional backend service.
* `max_rate_per_endpoint`: required/optional. Integer representing the maximum rate a pod can handle. Pick either rate or connection.
* `max_connections_per_endpoint`: required/optional. Integer representing the maximum amount of connections a pod can handle. Pick either rate or connection.
* `initial_capacity`: optional. Integer configuring the initial capacityScaler, expressed as a percentage between 0 and 100. If set to 0, the backend service will not receive any traffic until an operator or other service adjusts the [capacity scaler setting](https://cloud.google.com/load-balancing/docs/backend-service#capacity_scaler). Please note that unless you have existing backends in a backend service, you cannot set `initial_capacity` to zero (at least some backends have to higher than zero value).
* `capacity_scaler`: optional. Autoneg manages the [capacity scaler setting](https://cloud.google.com/load-balancing/docs/backend-service#capacity_scaler) if this and the `controller.autoneg.dev/sync: '{"capacity_scaler":true}'` annotation is set on the service. Please note updating `capacityScaler` setting out of band (eg. via `gcloud`) won't be overridden until you change the `capacity_scaler` (or other value) in the service configuration.

### Controller parameters

The controller parameters can be customized via changing the [controller deployment](https://github.com/GoogleCloudPlatform/gke-autoneg-controller/blob/master/deploy/autoneg.yaml#L217).

* `--enable-custom-service-names`: optional. Enables defining the backend service name in the `autoneg` annotation. If set to `false` (via 
  `--enable-custom-service-names=false`), the `name` option will be ignored in the `autoneg` annotation and the backend service name will
  be determined via `default-backendservice-name` (see below).
* `--default-backendservice-name`: optional. Sets the backend service name if it's not specified or if `--enable-customer-service-names` is set 
  to false. The template defaults to `{name}-{port}`. It can contain `namespace`, `name`, `port` and `hash` and the non-hash values will be 
  truncated evenly if the full name is longer than 63 characters. `<hash>` is generated using full length `namespace`, `name` and 
  `port` to avoid name collisions when truncated.
* `--max-rate-per-endpoint`: optional. Sets a default value for max-rate-per-endpoint that can be overridden by user config. Defaults to 0.
* `--max-connections-per-endpoint`: optional. Same as above but for connections.
* `--namespaces`: optional. Comma-separated list of namespaces to reconcile.
* `--always-reconcile`: optional. Makes it possible to reconcile periodically even if the status annotations don't change. Defaults to false.
* `--reconcile-period`: optional. Sets a reconciliation duration if always-reconcile mode is on. Defaults to 10 hours.

## IAM considerations

As `autoneg` is accessing GCP APIs, you must ensure that the controller has authorization to call those APIs. 
To follow the principle of least privilege, it is recommended that you configure your cluster with 
[Workload Identity](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity) to limit 
permissions to a GCP service account that `autoneg` operates under. 
If you choose not to use [Workload Identity](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity), 
you will need to create your GKE cluster with the "cloud-platform" scope.

## Security considerations

* Since the GKE cluster will require IAM permissions to manipulate the backend services in the project, 
  users may be able to register their services into any available backend service in the project. You 
  can enforce the allowed backends by disabling `--enable-custom-service-names` and customizing the 
  backend service name template.

## Installation

First, set up the GCP resources necessary to support [Workload Identity](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity), 
run the script:
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

Lastly, on each cluster in your project where you'd like to install `autoneg` (version `v1.1.0`), run these two commands:
```
kubectl apply -f deploy/autoneg.yaml

kubectl annotate sa -n autoneg-system autoneg-controller-manager \
  iam.gke.io/gcp-service-account=autoneg-system@${PROJECT_ID}.iam.gserviceaccount.com
```
This will create all the Kubernetes resources required to support `autoneg` and annotate the default service account in the `autoneg-system` namespace to associate a GCP service account using [Workload Identity](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity). 

### Installation via Terraform

You can use the Terraform module in `terraform/autoneg` to deploy Autoneg in a GKE cluster of your choice.
An end-to-end example is provided in the [`terraform/test`](terraform/test) directory as well (simply set your `project_id`).

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
  
  # NOTE: You may need to build your own image if you rely on features merged between releases, and do
  # not wish to use the `latest` image.
  controller_image = "ghcr.io/googlecloudplatform/gke-autoneg-controller/gke-autoneg-controller:v1.1.0"
}
```

### Installation via Helm charts

A Helm chart is also provided in [`deploy/chart`](deploy/chart) and via
`https://googlecloudplatform.github.io/gke-autoneg-controller/` repository. 

You can also use it with Terraform like this:

```tf
module "autoneg" {
  source = "github.com/GoogleCloudPlatform/gke-autoneg-controller//terraform/gcp?ref=master"

  project_id         = module.project.project_id
  service_account_id = "autoneg"
  workload_identity = {
    namespace       = "autoneg-system"
    service_account = "autoneg-controller-manager"
  }
  # To add shared VPC configuration, also set shared_vpc variable
}

resource "helm_release" "autoneg" {
  name       = "autoneg"
  chart      = "autoneg-controller-manager"
  repository = "https://googlecloudplatform.github.io/gke-autoneg-controller/"
  namespace  = "autoneg-system"

  create_namespace = true

  set {
    name  = "createNamespace"
    value = false
  }
  
  set {
    name  = "serviceAccount.annotations.iam\\.gke\\.io/gcp-service-account"
    value = module.autoneg.service_account_email
  }

  set {
    name  = "serviceAccount.automountServiceAccountToken"
    value = true
  }
}
```

### Customizing your installation

`autoneg` is based on [Kubebuilder](https://kubebuilder.io), and as such, you can customize and 
deploy `autoneg` according to the Kubebuilder "Run It On the Cluster" section of the 
[Quick Start](https://kubebuilder.io/quick-start.html#run-it-on-the-cluster). `autoneg` does not define
 a CRD, so you can skip any Kubebuilder steps involving CRDs.

The included `deploy/autoneg.yaml` is the default output of Kubebuilder's `make deploy` step, 
coupled with a public image.

Do keep in mind the additional configuration to enable [Workload Identity](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity).
