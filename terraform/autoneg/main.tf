/**
 * Copyright 2021 Google LLC
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

terraform {
  required_providers {
    google = {
      source = "hashicorp/google"
    }
    kubernetes = {
      source = "hashicorp/kubernetes"
    }
  }
}

module "gcp" {
  source = "../gcp"

  project_id                    = var.project_id
  service_account_id            = var.service_account_id
  shared_vpc                    = var.shared_vpc
  workload_identity             = var.workload_identity
  regional                      = var.regional
  custom_role_add_random_suffix = var.custom_role_add_random_suffix
}

module "kubernetes" {
  source = "../kubernetes"

  project_id            = var.project_id
  controller_image      = var.controller_image
  image_pull_policy     = var.image_pull_policy
  kube_rbac_proxy_image = var.kube_rbac_proxy_image
  namespace             = var.namespace
  service_account_id    = var.service_account_id
  workload_identity     = var.workload_identity
  priority_class_name   = var.priority_class_name
  replicas              = var.replicas
  pod_disruption_budget = var.pod_disruption_budget
}
