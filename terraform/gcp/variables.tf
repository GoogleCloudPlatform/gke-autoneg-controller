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
  description = "Service account id to be created"
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
  validation {
    condition = var.workload_identity != null ? (
      var.workload_identity.namespace != null &&
      var.workload_identity.service_account != null
    ) : true
    error_message = "When workload_identity is set, both namespace and service_account must be specified."
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
