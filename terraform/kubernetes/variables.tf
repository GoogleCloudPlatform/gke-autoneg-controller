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
}

variable "image_pull_policy" {
  type        = string
  description = "Image pull policy for Autoneg container"
  default     = "IfNotPresent"
}

variable "namespace" {
  type        = string
  description = "Autoneg namespace"
  default     = "autoneg-system"
}

variable "service_account_id" {
  type        = string
  description = "Autoneg service account"
  default     = "autoneg"
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
  validation {
    condition = var.workload_identity != null ? (
      var.workload_identity.namespace != null &&
      var.workload_identity.service_account != null
    ) : true
    error_message = "When workload_identity is set, both namespace and service_account must be specified."
  }
}

variable "priority_class_name" {
  description = "Pod's PriorityClass name"
  type        = string
  default     = null
}

variable "replicas" {
  description = "Number of replicas for the deployment"
  type        = number
  default     = 2
}

variable "pod_disruption_budget" {
  description = "Pod Disruption Budget configuration"
  type = object({
    enabled         = bool
    min_available   = optional(number)
    max_unavailable = optional(number)
  })
  default = {
    enabled         = true
    min_available   = 1
    max_unavailable = null
  }
  validation {
    condition     = var.pod_disruption_budget.enabled ? (var.pod_disruption_budget.min_available != null || var.pod_disruption_budget.max_unavailable != null) : true
    error_message = "When pod_disruption_budget is enabled, at least one of min_available or max_unavailable must be set"
  }
  validation {
    condition     = var.pod_disruption_budget.enabled ? !(var.pod_disruption_budget.min_available != null && var.pod_disruption_budget.max_unavailable != null) : true
    error_message = "When pod_disruption_budget is enabled, only one of min_available or max_unavailable can be set"
  }
}

variable "metrics_service" {
  description = "Create service for metrics"
  type        = bool
  default     = false
}

variable "autopilot" {
  description = "Whether the GKE cluster is Autopilot"
  type        = bool
  default     = false
}