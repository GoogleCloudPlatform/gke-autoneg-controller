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
  description = "Service account id to be created"
  default     = "autoneg"
  type        = string
}

variable "shared_vpc" {
  description = "Shared VPC configuration which the autoneg service account can use"
  default     = null
  type = object({
    project_id        = string
    subnetwork_region = string
    subnetwork_id     = string
  })
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

variable "regional" {
  description = "Whether or not to add regionBackend and regionHealthCheck permissions."
  default     = true
  type        = bool
}

variable "custom_role_add_random_suffix" {
  type        = bool
  description = "Sets random suffix at the end of the IAM custom role id"
  default     = false
}
