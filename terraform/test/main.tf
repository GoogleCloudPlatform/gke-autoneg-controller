/**
 * Copyright 2023 Google LLC
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
  project_id      = var.project_create ? module.project.project_id : var.project_id
  ilb_name        = "autoneg-test-ilb"
  backend_service = "autoneg-test-be"
}

module "project" {
  source         = "github.com/GoogleCloudPlatform/cloud-foundation-fabric//modules/project?ref=daily-2023.03.14"
  name           = var.project_id
  project_create = var.project_create
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
  source     = "github.com/GoogleCloudPlatform/cloud-foundation-fabric//modules/net-vpc?ref=daily-2023.03.14"
  project_id = var.vpc_config.network_project != null ? var.vpc_config.network_project : local.project_id
  name       = var.vpc_config.network
  subnets = var.vpc_config.create ? [{
    ip_cidr_range = var.vpc_subnets.main_cidr_range
    name          = var.vpc_config.subnetwork
    region        = var.region
    secondary_ip_ranges = {
      (var.vpc_subnets.pods_name)     = var.vpc_subnets.pods_ip_cidr_range
      (var.vpc_subnets.services_name) = var.vpc_subnets.services_ip_cidr_range
    }

  }] : []
  subnets_proxy_only = [
    {
      ip_cidr_range = var.vpc_subnets.proxy_only_cidr_range
      name          = format("%s-proxy", var.vpc_config.network)
      region        = var.region
      active        = true
    }
  ]
  vpc_create = var.vpc_config.create
}

module "nat" {
  source         = "github.com/GoogleCloudPlatform/cloud-foundation-fabric//modules/net-cloudnat?ref=daily-2023.03.14"
  project_id     = local.project_id
  region         = var.region
  name           = format("%s-nat", module.vpc.name)
  router_network = module.vpc.name
}

module "firewall" {
  source               = "github.com/GoogleCloudPlatform/cloud-foundation-fabric//modules/net-vpc-firewall?ref=daily-2023.03.14"
  project_id           = local.project_id
  network              = module.vpc.name
  default_rules_config = {}
  ingress_rules = {
    allow-ingress-from-ilb = {
      description   = "Allow ingress from ILB"
      source_ranges = [var.vpc_subnets.proxy_only_cidr_range]
      targets       = ["autoneg-test"]
      rules         = [{ protocol = "tcp", port = 80 }]
    }
    allow-ingress-healthchecks = {
      description   = "Allow healthcheck ranges."
      source_ranges = ["35.191.0.0/16", "130.211.0.0/22"]
      targets       = ["autoneg-test"]
      rules         = [{ protocol = "tcp" }]
    }
  }
}

module "cluster-service-account" {
  source       = "github.com/GoogleCloudPlatform/cloud-foundation-fabric//modules/iam-service-account?ref=daily-2023.03.14"
  project_id   = local.project_id
  name         = format("autoneg-test-sa")
  generate_key = false
  iam          = {}
  iam_project_roles = {
    (local.project_id) = [
      "roles/container.nodeServiceAccount",
      "roles/artifactregistry.reader"
    ]
  }
}

module "cluster" {
  source = "github.com/GoogleCloudPlatform/cloud-foundation-fabric//modules/gke-cluster?ref=daily-2023.03.14"

  project_id = local.project_id
  name       = "autoneg-test"
  location   = var.region

  release_channel = "REGULAR"

  vpc_config = {
    network    = module.vpc.self_link
    subnetwork = module.vpc.subnet_self_links[format("%s/%s", var.region, var.vpc_config.subnetwork)]
    secondary_range_names = {
      pods     = var.vpc_subnets.pods_name
      services = var.vpc_subnets.services_name
    }
    master_ipv4_cidr_block = var.vpc_subnets.master_ipv4_cidr_block
    master_authorized_ranges = {
      internal-vms = "0.0.0.0/0"
    }
  }
  max_pods_per_node = 32

  private_cluster_config = {
    enable_private_endpoint = false
    master_global_access    = false
  }

  enable_features = {
    dataplane_v2      = true
    workload_identity = true
  }

  labels = {
    environment = "test"
  }
}

module "cluster-nodepool" {
  source     = "github.com/GoogleCloudPlatform/cloud-foundation-fabric//modules/gke-nodepool?ref=daily-2023.03.14"
  project_id = local.project_id

  cluster_name = module.cluster.name
  location     = module.cluster.location
  name         = "autoneg-test-nodepool-1"

  service_account = {
    email        = module.cluster-service-account.email
    oauth_scopes = ["https://www.googleapis.com/auth/cloud-platform"]
  }

  node_config = {
    machine_type = "e2-standard-4"
    gvnic        = true
  }
  node_count = {
    initial = 1
  }

  tags = ["autoneg-test"]
}

data "google_client_config" "provider" {}

provider "kubernetes" {
  host  = format("https://%s", module.cluster.endpoint)
  token = data.google_client_config.provider.access_token
  cluster_ca_certificate = base64decode(
    module.cluster.ca_certificate,
  )
}

module "autoneg" {
  source = "github.com/GoogleCloudPlatform/gke-autoneg-controller//terraform/autoneg?ref=kubebuilder3"

  project_id = local.project_id

  controller_image = var.autoneg_image

  depends_on = [
    module.cluster-nodepool.name
  ]
}

module "ilb" {
  source     = "github.com/GoogleCloudPlatform/cloud-foundation-fabric//modules/net-ilb-l7?ref=daily-2023.03.14"
  name       = local.ilb_name
  project_id = local.project_id
  region     = var.region

  backend_service_configs = {
    (local.backend_service) = {
      backends = []
    }
  }
  urlmap_config = {
    default_service = local.backend_service
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
    subnetwork = module.vpc.subnet_self_links[format("%s/%s", var.region, var.vpc_config.subnetwork)]
  }
}

resource "kubernetes_deployment" "hello-deployment" {
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
    module.autoneg
  ]
}

resource "kubernetes_service" "hello-workload" {
  metadata {
    name      = "hello-service"
    namespace = "default"
    annotations = {
      "controller.autoneg.dev/neg" = jsonencode({
        backend_services = {
          "80" = [{
            name                  = format("%s-%s", local.ilb_name, local.backend_service)
            max_rate_per_endpoint = 100
            region                = var.region
          }]
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
    selector = kubernetes_deployment.hello-deployment.metadata[0].labels

    port {
      protocol    = "TCP"
      name        = "http"
      port        = 80
      target_port = 8000
    }
    type = "ClusterIP"
  }
}