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

variable "controller_image" {
  type        = string
  description = "Autoneg controller container image"
  default     = "ghcr.io/googlecloudplatform/gke-autoneg-controller/gke-autoneg-controller:latest"
}

variable "image_pull_policy" {
  type        = string
  description = "Image pull policy for Autoneg container"
  default     = "IfNotPresent"
}

variable "kube_rbac_proxy_image" {
  type        = string
  description = "kuber-rbac-proxy container image"
  default     = "gcr.io/kubebuilder/kube-rbac-proxy:v0.8.0"
}

variable "workload_identity" {
  description = "Workload identity configuration"
  type = object({
    namespace       = string
    service_account = string
  })
  default = {
    namespace       = "autoneg-system"
    service_account = "autoneg"
  }
}

variable "custom_role_add_random_suffix" {
  type        = bool
  description = "Sets random suffix at the end of the IAM custom role id"
  default     = false
}

variable "service_account_id" {
  description = "Service account id to be created"
  default     = "autoneg"
  type        = string
}

variable "priority_class_name" {
  description = "Pod's PriorityClass name"
  type        = string
  default     = null
}
