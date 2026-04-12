/**
 * Copyright 2024 Google LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

locals {
  project_id                    = var.project_create ? module.project.project_id : var.project_id
  suffix                        = var.add_suffix
  ilb_name_primary              = format("autoneg-test-primary-ilb%s", local.suffix)
  ilb_name_secondary            = format("autoneg-test-secondary-ilb%s", local.suffix)
  xlb_name                      = format("autoneg-test-xlb%s", local.suffix)
  ilb_backend_service_primary   = format("be%s", local.suffix)
  ilb_backend_service_secondary = format("be%s", local.suffix)

  xlb_backend_service = format("be%s", local.suffix)
}

module "project" {
  source = "github.com/GoogleCloudPlatform/cloud-foundation-fabric//modules/project?ref=v54.3.0"
  name   = var.project_id
  project_reuse = var.project_create == true ? null : {
    use_data_source = true
  }
  services = [
    "container.googleapis.com",
    "cloudresourcemanager.googleapis.com",
    "compute.googleapis.com",
    "trafficdirector.googleapis.com",
    "networkservices.googleapis.com",
    "networksecurity.googleapis.com",
    "privateca.googleapis.com",
    "gkehub.googleapis.com"
  ]
}

module "vpc" {
  source     = "github.com/GoogleCloudPlatform/cloud-foundation-fabric//modules/net-vpc?ref=v54.3.0"
  project_id = var.vpc_config.network_project != null ? var.vpc_config.network_project : local.project_id
  name       = format("%s%s", var.vpc_config.network, local.suffix)
  subnets = var.vpc_config.create ? concat([{
    ip_cidr_range = var.vpc_subnets.primary.main_cidr_range
    name          = format("%s%s", var.vpc_config.subnetwork_primary, local.suffix)
    region        = var.region
    secondary_ip_ranges = {
      (var.vpc_subnets.primary.pods_name) = { ip_cidr_range = var.vpc_subnets.primary.pods_ip_cidr_range }
    }
    }], var.secondary_region != null ? [
    {
      ip_cidr_range = var.vpc_subnets.secondary.main_cidr_range
      name          = format("%s%s", var.vpc_config.subnetwork_secondary, local.suffix)
      region        = var.secondary_region
      secondary_ip_ranges = {
        (var.vpc_subnets.secondary.pods_name) = { ip_cidr_range = var.vpc_subnets.secondary.pods_ip_cidr_range }
      }
    }
  ] : []) : []
  subnets_proxy_only = concat([
    {
      ip_cidr_range = var.vpc_subnets.primary.proxy_only_cidr_range
      name          = format("%s-primary-proxy%s", var.vpc_config.network, local.suffix)
      region        = var.region
      active        = true
    }
    ], var.secondary_region != null ? [
    {
      ip_cidr_range = var.vpc_subnets.secondary.proxy_only_cidr_range
      name          = format("%s-secondary-proxy%s", var.vpc_config.network, local.suffix)
      region        = var.secondary_region
      active        = true
    }
  ] : [])
  vpc_reuse = var.vpc_config.create ? null : {
    use_data_source = true
  }
}

module "nat-primary" {
  source         = "github.com/GoogleCloudPlatform/cloud-foundation-fabric//modules/net-cloudnat?ref=v54.3.0"
  project_id     = local.project_id
  region         = var.region
  name           = format("%s-primary-nat%s", module.vpc.name, local.suffix)
  router_network = module.vpc.name
}

module "nat-secondary" {
  for_each       = var.secondary_region != null ? toset([""]) : toset([])
  source         = "github.com/GoogleCloudPlatform/cloud-foundation-fabric//modules/net-cloudnat?ref=v54.3.0"
  project_id     = local.project_id
  region         = var.secondary_region
  name           = format("%s-secondary-nat%s", module.vpc.name, local.suffix)
  router_network = module.vpc.name
}

module "firewall" {
  source               = "github.com/GoogleCloudPlatform/cloud-foundation-fabric//modules/net-vpc-firewall?ref=v54.3.0"
  project_id           = local.project_id
  network              = module.vpc.name
  default_rules_config = {}
  ingress_rules = merge({
    (format("allow-ingress-from-ilb%s", local.suffix)) = {
      description   = "Allow ingress from ILB"
      source_ranges = concat([var.vpc_subnets.primary.proxy_only_cidr_range], var.secondary_region != null ? [var.vpc_subnets.secondary.proxy_only_cidr_range] : [])
      targets       = ["autoneg-test"]
      rules         = [{ protocol = "tcp", port = 80 }]
    }
    (format("allow-ingress-healthchecks%s", local.suffix)) = {
      description   = "Allow healthcheck ranges."
      source_ranges = ["35.191.0.0/16", "130.211.0.0/22"]
      targets       = ["autoneg-test"]
      rules         = [{ protocol = "tcp" }]
    }
    }, var.create_xlb == true ? {
    (format("allow-ingress-xlb%s", local.suffix)) = {
      description   = "Allow traffic from XLB ranges."
      source_ranges = ["35.191.0.0/16", "130.211.0.0/22"]
      targets       = ["autoneg-test"]
      rules         = [{ protocol = "tcp" }]
    }
  } : {})
}

module "cluster-service-account" {
  source     = "github.com/GoogleCloudPlatform/cloud-foundation-fabric//modules/iam-service-account?ref=v54.3.0"
  project_id = local.project_id
  name       = format("autoneg-test-sa%s", local.suffix)
  iam        = {}
  iam_project_roles = {
    (local.project_id) = [
      "roles/container.nodeServiceAccount",
      "roles/artifactregistry.reader"
    ]
  }
}

module "cluster-primary" {
  for_each = toset([""])
  source   = "github.com/GoogleCloudPlatform/cloud-foundation-fabric//modules/gke-cluster-autopilot?ref=v54.3.0"

  project_id = local.project_id
  name       = format("autoneg-test-primary%s", local.suffix)
  location   = var.region

  release_channel = "REGULAR"

  vpc_config = {
    network    = module.vpc.self_link
    subnetwork = module.vpc.subnet_self_links[format("%s/%s%s", var.region, var.vpc_config.subnetwork_primary, local.suffix)]
    secondary_range_names = {
      pods = var.vpc_subnets.primary.pods_name
    }
  }

  access_config = {
    private_nodes = true
    dns_access    = {}
  }

  node_config = {
    service_account = module.cluster-service-account.email
    tags            = ["autoneg-test"]
  }

  enable_features = {
    dataplane_v2      = true
    workload_identity = true
  }

  deletion_protection = false

  labels = {
    environment = "test"
  }
}

module "cluster-secondary" {
  for_each = var.secondary_region != null ? toset([""]) : toset([])
  source   = "github.com/GoogleCloudPlatform/cloud-foundation-fabric//modules/gke-cluster-autopilot?ref=v54.3.0"

  project_id = local.project_id
  name       = format("autoneg-test-secondary%s", local.suffix)
  location   = var.secondary_region

  release_channel = "REGULAR"

  vpc_config = {
    network    = module.vpc.self_link
    subnetwork = module.vpc.subnet_self_links[format("%s/%s%s", var.secondary_region, var.vpc_config.subnetwork_secondary, local.suffix)]
    secondary_range_names = {
      pods = var.vpc_subnets.secondary.pods_name
    }
  }

  access_config = {
    private_nodes = true
    dns_access    = {}
  }

  node_config = {
    service_account = module.cluster-service-account.email
    tags            = ["autoneg-test"]
  }

  enable_features = {
    dataplane_v2      = true
    workload_identity = true
  }

  deletion_protection = false

  labels = {
    environment = "test"
  }
}

## Kubernetes resources

data "google_client_config" "provider" {}

provider "kubernetes" {
  alias = "primary"

  host  = format("https://%s", module.cluster-primary[""].dns_endpoint)
  token = data.google_client_config.provider.access_token
}

provider "kubernetes" {
  alias = "secondary"

  host  = var.secondary_region != null ? format("https://%s", module.cluster-secondary[""].dns_endpoint) : null
  token = data.google_client_config.provider.access_token
}

module "autoneg-primary" {
  for_each = toset([""])

  providers = {
    kubernetes = kubernetes.primary
  }

  source = "../autoneg"

  project_id                    = local.project_id
  service_account_id            = format("autoneg-primary%s", local.suffix)
  controller_image              = var.autoneg_image
  custom_role_add_random_suffix = local.suffix != "" ? true : false

  manager_configuration = var.manager_configuration

  depends_on = [
    module.cluster-primary
  ]
}

module "autoneg-secondary" {
  for_each = var.secondary_region != null ? toset([""]) : toset([])

  providers = {
    kubernetes = kubernetes.secondary
  }

  source = "../autoneg"

  project_id                    = local.project_id
  service_account_id            = format("autoneg-secondary%s", local.suffix)
  controller_image              = var.autoneg_image
  custom_role_add_random_suffix = local.suffix != "" ? true : false

  manager_configuration = var.manager_configuration

  depends_on = [
    module.cluster-secondary
  ]
}

module "ilb-primary" {
  for_each   = var.create_ilb ? toset([""]) : toset([])
  source     = "github.com/GoogleCloudPlatform/cloud-foundation-fabric//modules/net-lb-app-int?ref=v54.3.0"
  name       = local.ilb_name_primary
  project_id = local.project_id
  region     = var.region

  backend_service_configs = {
    (local.ilb_backend_service_primary) = {
      backends = []
    }
  }
  urlmap_config = {
    default_service = local.ilb_backend_service_primary
  }

  health_check_configs = {
    default = {
      project_id = local.project_id
      http = {
        port_specification = "USE_SERVING_PORT"
      }
    }
  }

  vpc_config = {
    network    = module.vpc.self_link
    subnetwork = module.vpc.subnet_self_links[format("%s/%s%s", var.region, var.vpc_config.subnetwork_primary, local.suffix)]
  }
}

module "ilb-secondary" {
  for_each   = var.create_ilb && var.secondary_region != null ? toset([""]) : toset([])
  source     = "github.com/GoogleCloudPlatform/cloud-foundation-fabric//modules/net-lb-app-int?ref=v54.3.0"
  name       = local.ilb_name_secondary
  project_id = local.project_id
  region     = var.secondary_region

  backend_service_configs = {
    (local.ilb_backend_service_secondary) = {
      backends = []
    }
  }
  urlmap_config = {
    default_service = local.ilb_backend_service_secondary
  }

  health_check_configs = {
    default = {
      project_id = local.project_id
      http = {
        port_specification = "USE_SERVING_PORT"
      }
    }
  }

  vpc_config = {
    network    = module.vpc.self_link
    subnetwork = module.vpc.subnet_self_links[format("%s/%s%s", var.secondary_region, var.vpc_config.subnetwork_secondary, local.suffix)]
  }
}

module "xlb" {
  for_each   = var.create_xlb ? toset([""]) : toset([])
  source     = "github.com/GoogleCloudPlatform/cloud-foundation-fabric//modules/net-lb-app-ext?ref=v54.3.0"
  name       = local.xlb_name
  project_id = local.project_id

  use_classic_version = false

  backend_service_configs = {
    (local.xlb_backend_service) = {
      backends = []
    }
  }
  urlmap_config = {
    default_service = local.xlb_backend_service
  }

  health_check_configs = {
    default = {
      project_id = local.project_id
      http = {
        port_specification = "USE_SERVING_PORT"
      }
    }
  }
}

resource "kubernetes_deployment_v1" "hello-deployment-primary" {
  for_each = toset([""])
  provider = kubernetes.primary

  metadata {
    name      = "hello-deployment"
    namespace = "default"
    labels = {
      app = "hello"
    }
  }

  spec {
    replicas = 3

    selector {
      match_labels = {
        app = "hello"
      }
    }

    template {
      metadata {
        labels = {
          app = "hello"
        }
      }

      spec {
        security_context {
          fs_group = 1337
        }
        container {
          image = "docker.io/waip/simple-http:v1.0.1"
          name  = "hello"

          port {
            container_port = 8000
          }
        }
      }
    }
  }

  depends_on = [
    module.autoneg-primary
  ]
}

resource "kubernetes_service_v1" "hello-workload-primary" {
  for_each = toset([""])
  provider = kubernetes.primary

  metadata {
    name      = "hello-service"
    namespace = "default"
    annotations = {
      "controller.autoneg.dev/neg" = jsonencode({
        backend_services = {
          "80" = concat(var.create_ilb == true ? [{
            name                  = format("%s-%s", local.ilb_name_primary, local.ilb_backend_service_primary)
            max_rate_per_endpoint = 100
            region                = var.region
            }] : [], var.create_xlb == true ? [{
            name                  = format("%s-%s", local.xlb_name, local.xlb_backend_service)
            max_rate_per_endpoint = 100
          }] : [])
        }
      })
      "cloud.google.com/neg" = jsonencode({
        exposed_ports = {
          "80" = {}
        }
      })
    }
  }
  spec {
    selector = kubernetes_deployment_v1.hello-deployment-primary[""].metadata[0].labels

    port {
      protocol    = "TCP"
      name        = "http"
      port        = 80
      target_port = 8000
    }
    type = "ClusterIP"
  }
}

resource "kubernetes_deployment_v1" "hello-deployment-secondary" {
  for_each = toset([""])
  provider = kubernetes.secondary

  metadata {
    name      = "hello-deployment"
    namespace = "default"
    labels = {
      app = "hello"
    }
  }

  spec {
    replicas = 3

    selector {
      match_labels = {
        app = "hello"
      }
    }

    template {
      metadata {
        labels = {
          app = "hello"
        }
      }

      spec {
        security_context {
          fs_group = 1337
        }
        container {
          image = "docker.io/waip/simple-http:v1.0.1"
          name  = "hello"

          port {
            container_port = 8000
          }
        }
      }
    }
  }

  depends_on = [
    module.autoneg-secondary
  ]
}

resource "kubernetes_service_v1" "hello-workload-secondary" {
  for_each = toset([""])
  provider = kubernetes.secondary

  metadata {
    name      = "hello-service"
    namespace = "default"
    annotations = {
      "controller.autoneg.dev/neg" = jsonencode({
        backend_services = {
          "80" = concat(var.create_ilb == true ? [{
            name                  = format("%s-%s", local.ilb_name_secondary, local.ilb_backend_service_secondary)
            max_rate_per_endpoint = 100
            region                = var.secondary_region
            }] : [], var.create_xlb == true ? [{
            name                  = format("%s-%s", local.xlb_name, local.xlb_backend_service)
            max_rate_per_endpoint = 100
          }] : [])
        }
      })
      "cloud.google.com/neg" = jsonencode({
        exposed_ports = {
          "80" = {}
        }
      })
    }
  }
  spec {
    selector = kubernetes_deployment_v1.hello-deployment-secondary[""].metadata[0].labels

    port {
      protocol    = "TCP"
      name        = "http"
      port        = 80
      target_port = 8000
    }
    type = "ClusterIP"
  }
}
