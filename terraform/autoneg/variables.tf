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

variable "project_id" {
  type        = string
  description = "GCP project ID"
}

variable "service_account_id" {
  type        = string
  description = "Autoneg service account"
  default     = "autoneg"
}

variable "shared_vpc" {
  type = object({
    project_id        = string
    subnetwork_region = string
    subnetwork_id     = string
  })
  description = "Shared VPC configuration which the autoneg service account can use"
  default     = null
}

variable "workload_identity" {
  type = object({
    namespace       = string
    service_account = string
  })
  description = "Workload identity configuration"
  default = {
    namespace       = "autoneg-system"
    service_account = "autoneg"
  }
}

variable "regional" {
  type        = bool
  description = "Whether or not to add regionBackend and regionHealthCheck permissions."
  default     = true
}

variable "custom_role_add_random_suffix" {
  type        = bool
  description = "Sets random suffix at the end of the IAM custom role id"
  default     = false
}

variable "controller_image" {
  type        = string
  description = "Autoneg controller container image"
  default     = "ghcr.io/googlecloudplatform/gke-autoneg-controller/gke-autoneg-controller:v2.0.0"
}

variable "image_pull_policy" {
  type        = string
  description = "Image pull policy for Autoneg container"
  default     = "IfNotPresent"
}

variable "kube_rbac_proxy_image" {
  type        = string
  description = "kuber-rbac-proxy container image"
  default     = "gcr.io/kubebuilder/kube-rbac-proxy:v0.16.0"
}

variable "namespace" {
  type        = string
  description = "Autoneg namespace"
  default     = "autoneg-system"
}

variable "priority_class_name" {
  type        = string
  description = "Pod's PriorityClass name"
  default     = null
}

variable "replicas" {
  type        = number
  description = "Number of replicas for the deployment"
  default     = 1
}

variable "pod_disruption_budget" {
  type = object({
    enabled         = bool
    min_available   = optional(number)
    max_unavailable = optional(number)
  })
  description = "Pod Disruption Budget configuration"
  default = {
    enabled = false
  }
}
