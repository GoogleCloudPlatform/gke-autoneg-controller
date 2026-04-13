#!/usr/bin/env python3
# Copyright 2026 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
import kubernetes.client
from kubernetes.client.rest import ApiException
import os
import sys
import subprocess
import types
import google.auth
import google.auth.transport.requests
from google.cloud import logging_v2, compute_v1
import json
from datetime import datetime, timezone
from timelength import TimeLength
import proto
from collections import namedtuple, Counter
from typing import Callable
import traceback
from termcolor import colored
from time import sleep
import warnings

warnings.filterwarnings(
    "ignore",
    "Your application has authenticated using end user credentials from Google Cloud SDK without a quota project."
)

type TestCallback = Callable[types.SimpleNamespace, kubernetes.client.ApiClient, kubernetes.client.ApiClient, float]
BackendServiceRef = namedtuple("BackendServiceRef", ["project_id", "region", "name"])
class TestTimeoutException(Exception):
    pass

def parse_backend_service(backend_service: str) -> BackendServiceRef:
    be = backend_service.split("/")
    if be[2] != "global": 
        return BackendServiceRef(be[1], be[3], be[5])
    return BackendServiceRef(be[1], be[2], be[4])

def get_backend_service(backend_service: str) -> compute_v1.types.compute.BackendService:
    parsed = parse_backend_service(backend_service)
    if parsed.region == "global":
        client = compute_v1.BackendServicesClient()
        request = compute_v1.GetBackendServiceRequest(
            project=parsed.project_id,
            backend_service=parsed.name,
        )

        response = client.get(request=request)
        return response
    else:
        client = compute_v1.RegionBackendServicesClient()
        request = compute_v1.GetRegionBackendServiceRequest(
            backend_service=parsed.name,
            project=parsed.project_id,
            region=parsed.region,
        )

        response = client.get(request=request)
        return response

def get_backend_service_backends(backend_service: str) -> list[str]:
    backend_service = get_backend_service(backend_service)
    backends = []
    for backend in backend_service.backends:
        backends.append(os.path.basename(backend.group))
    return backends

def collect_autoneg_manager_logs(look_back: str, config: types.SimpleNamespace) -> dict[list[dict]]:
    client = logging_v2.services.logging_service_v2.LoggingServiceV2Client()

    tl = TimeLength(look_back)
    timestamp = tl.ago(base=datetime.now(timezone.utc)).isoformat()
    
    logs = {
        "primary": [],
        "secondary": []
    }
    for cluster in logs.keys():
        cluster_name = config.primary_cluster_name if cluster == "primary" else config.secondary_cluster_name
        
        search_filter = f"timestamp >= \"{timestamp}\" AND resource.labels.cluster_name=\"{cluster_name}\" AND resource.labels.namespace_name=\"autoneg-system\""
        request = logging_v2.types.ListLogEntriesRequest(
            resource_names=[f"projects/{config.project_id}"],
            filter=search_filter
        )

        # Make the request
        page_result = client.list_log_entries(request=request)

        # Handle the response
        for response in page_result:
            converted_log_entry = proto.Message.to_dict(response)
            logs[cluster].append(converted_log_entry)
    return logs

def get_primary_kubernetes_client(api_token: str, config: types.SimpleNamespace) -> kubernetes.client.ApiClient:
    credentials, project = google.auth.default()
    credentials.refresh(google.auth.transport.requests.Request())
    
    configuration = kubernetes.client.Configuration()
    configuration.api_key["authorization"] = api_token
    configuration.api_key_prefix["authorization"] = 'Bearer'
    configuration.host = config.primary_cluster

    return kubernetes.client.ApiClient(configuration) 

def get_secondary_kubernetes_client(api_token: str, config: types.SimpleNamespace) -> kubernetes.client.ApiClient:
    configuration = kubernetes.client.Configuration()
    configuration.api_key["authorization"] = api_token
    configuration.api_key_prefix["authorization"] = 'Bearer'
    configuration.host = config.secondary_cluster

    return kubernetes.client.ApiClient(configuration) 

def get_service(api_client: kubernetes.client.ApiClient, namespace: str, name: str):
    api_instance = kubernetes.client.CoreV1Api(api_client)
    return api_instance.read_namespaced_service(name, namespace)
    

def get_available_negs_from_service(api_client: kubernetes.client.ApiClient, namespace: str, name: str, port: int) -> list[str] | None:
    api_instance = kubernetes.client.CoreV1Api(api_client)
    service = api_instance.read_namespaced_service(name, namespace)
    if "cloud.google.com/neg-status" in service.metadata.annotations:
        parsed_negs = json.loads(service.metadata.annotations["cloud.google.com/neg-status"])
        return [parsed_negs["network_endpoint_groups"][str(port)]]
    return None        

def patch_service(api_client: kubernetes.client.ApiClient, namespace: str, name: str, service: kubernetes.client.models.V1Service, force: bool = None) -> kubernetes.client.models.V1Service | None:
    api_instance = kubernetes.client.CoreV1Api(api_client)
    service.spec.selector["patch"] = datetime.now(timezone.utc).strftime("%Y%m%d%H%M%S")

    response = api_instance.patch_namespaced_service(name, namespace, service, force=force)
    return response


def initial_check(config: types.SimpleNamespace, primary_client: kubernetes.client.ApiClient, secondary_client: kubernetes.client.ApiClient, timeout: float) -> None:

    svc_on_primary = get_service(primary_client, config.primary_service[0], config.primary_service[1])
    svc_on_secondary = get_service(secondary_client, config.secondary_service[0], config.secondary_service[1])

    # Check config
    assert "controller.autoneg.dev/neg" in svc_on_primary.metadata.annotations
    assert "controller.autoneg.dev/neg" in svc_on_secondary.metadata.annotations

    idx = 0
    while idx <= timeout:
        if "controller.autoneg.dev/neg-status" in svc_on_primary.metadata.annotations and "controller.autoneg.dev/neg-status" in svc_on_secondary.metadata.annotations:
            break
        sleep(1)
        idx += 1

        svc_on_primary = get_service(primary_client, config.primary_service[0], config.primary_service[1])
        svc_on_secondary = get_service(secondary_client, config.secondary_service[0], config.secondary_service[1])
    if idx > timeout:
        raise TestTimeoutException("Timed out waiting for Autoneg status to be created")

    # Check status
    assert "controller.autoneg.dev/neg-status" in svc_on_primary.metadata.annotations
    assert "controller.autoneg.dev/neg-status" in svc_on_secondary.metadata.annotations

    primary_status = json.loads(svc_on_primary.metadata.annotations["controller.autoneg.dev/neg-status"])
    secondary_status = json.loads(svc_on_secondary.metadata.annotations["controller.autoneg.dev/neg-status"])

    # Check that the backend service is in the Autoneg status on the XLB
    assert config.xlb_backend_service_parsed.name in primary_status["backend_services"][str(config.service_port)]
    assert config.xlb_backend_service_parsed.name in secondary_status["backend_services"][str(config.service_port)]

    # Check that the region-specific backend services are in the Autoneg status on the ILBs
    assert config.ilb_primary_backend_service_parsed.name in primary_status["backend_services"][str(config.service_port)]
    assert config.ilb_secondary_backend_service_parsed.name in secondary_status["backend_services"][str(config.service_port)]

    available_negs_on_primary = get_available_negs_from_service(primary_client, config.primary_service[0], config.primary_service[1], config.service_port)
    available_negs_on_secondary = get_available_negs_from_service(secondary_client, config.secondary_service[0], config.secondary_service[1], config.service_port)

    # Check that the NEGs available match the NEGs in Autoneg status
    assert Counter([primary_status["network_endpoint_groups"][str(config.service_port)]]) == Counter(available_negs_on_primary)
    assert Counter([secondary_status["network_endpoint_groups"][str(config.service_port)]]) == Counter(available_negs_on_secondary)

def remove_alt_backend(config: types.SimpleNamespace, primary_client: kubernetes.client.ApiClient, secondary_client: kubernetes.client.ApiClient, timeout: float) -> None:
    svc_on_primary = get_service(primary_client, config.primary_service[0], config.primary_service[1])

    primary_config = json.loads(svc_on_primary.metadata.annotations["controller.autoneg.dev/neg"])

    # Check that the alternative backend is configured
    assert len(list(filter(lambda x: x["name"] == config.ilb_primary_alt_backend_service_parsed.name, primary_config["backend_services"][str(config.service_port)]))) == 1

    primary_backend_service_backends = get_backend_service_backends(config.ilb_primary_alt_backend_service)
    available_negs_on_primary = get_available_negs_from_service(primary_client, config.primary_service[0], config.primary_service[1], config.service_port)

    # Check that the correct NEGs have been added to the alternative backend service
    assert len(list(set(primary_backend_service_backends) & set(available_negs_on_primary))) > 0

    new_primary_config = primary_config
    new_primary_config["backend_services"][str(config.service_port)] = list(filter(lambda x: x["name"] != config.ilb_primary_alt_backend_service_parsed.name, primary_config["backend_services"][str(config.service_port)]))

    # Patch service and remove alternative backend service
    original_status = svc_on_primary.metadata.annotations["controller.autoneg.dev/neg-status"]
    new_primary_service = svc_on_primary
    new_primary_service.metadata.annotations["controller.autoneg.dev/neg"] = json.dumps(new_primary_config)
    patch_service(primary_client, config.primary_service[0], config.primary_service[1], new_primary_service)
    
    # Wait until the change has been actuated by Autoneg
    idx = 0
    while idx <= timeout:
        check_svc = get_service(primary_client, config.primary_service[0], config.primary_service[1])
        if check_svc.metadata.annotations["controller.autoneg.dev/neg-status"] != original_status:
            break
        sleep(1)
        idx += 1
    if idx > timeout:
        raise TestTimeoutException("Timed out waiting for Autoneg status annotation to change")

    idx = 0
    while idx <= timeout:
        alt_backends = get_backend_service_backends(config.ilb_primary_alt_backend_service)
        if len(alt_backends) == 0:
            break
        sleep(1)
        idx += 1
    if idx > timeout:
        raise TestTimeoutException("Timed out waiting for backends to be removed from the alternative service")

def check_xlb(config: types.SimpleNamespace, primary_client: kubernetes.client.ApiClient, secondary_client: kubernetes.client.ApiClient, timeout: float) -> None:
    xlb_backends = get_backend_service_backends(config.xlb_backend_service)
    assert len(xlb_backends) > 5

def remove_configuration(config: types.SimpleNamespace, primary_client: kubernetes.client.ApiClient, secondary_client: kubernetes.client.ApiClient, timeout: float) -> None:
    svc_on_primary = get_service(primary_client, config.primary_service[0], config.primary_service[1])
    svc_on_secondary = get_service(secondary_client, config.secondary_service[0], config.secondary_service[1])

    svc_on_primary.metadata.annotations["controller.autoneg.dev/neg"] = "{}"
    svc_on_secondary.metadata.annotations["controller.autoneg.dev/neg"] = "{}"

    patch_service(primary_client, config.primary_service[0], config.primary_service[1], svc_on_primary)
    patch_service(secondary_client, config.secondary_service[0], config.secondary_service[1], svc_on_secondary)

    idx = 0
    while idx <= timeout:
        primary_ilb_backends = get_backend_service_backends(config.ilb_primary_backend_service)
        primary_ilb_alt_backends = get_backend_service_backends(config.ilb_primary_alt_backend_service)
        secondary_ilb_backends = get_backend_service_backends(config.ilb_secondary_backend_service)
        secondary_ilb_alt_backends = get_backend_service_backends(config.ilb_secondary_alt_backend_service)
        if len(primary_ilb_backends) == 0 and len(primary_ilb_alt_backends) == 0 and len(secondary_ilb_backends) == 0 and len(secondary_ilb_alt_backends) == 0: 
            break
        sleep(1)
        idx += 1
    if idx > timeout:
        raise TestTimeoutException("Timed out waiting for backends to be removed from the ILB")

    while idx <= timeout:
        xlb_backends = get_backend_service_backends(config.xlb_backend_service)
        if len(xlb_backends) == 0:
            break
        sleep(1)
        idx += 1
    if idx > timeout:
        raise TestTimeoutException("Timed out waiting for backends to be removed from the XLB")

tests = [
    ("Test initial conditions", initial_check),
    ("Remove alternative backend from configuration", remove_alt_backend),
    ("Check XLB has been properly populated", check_xlb),
    ("Remove entire configuration", remove_configuration)
]

print("Loading Terraform output: ", file=sys.stderr, end="", flush=True)
terraform_output = subprocess.check_output("terraform output -json", stderr=subprocess.STDOUT, shell=True)
terraform_configs = json.loads(terraform_output)
print(colored("OK", "green"), file=sys.stderr, flush=True)

config = types.SimpleNamespace(
    project_id=terraform_configs["project_id"]["value"],
    primary_cluster=terraform_configs["primary_cluster"]["value"],
    secondary_cluster=terraform_configs["secondary_cluster"]["value"],
    primary_cluster_name=terraform_configs["primary_cluster_name"]["value"],
    secondary_cluster_name=terraform_configs["secondary_cluster_name"]["value"],
    primary_service=(terraform_configs["primary_service_namespace"]["value"], terraform_configs["primary_service_name"]["value"]),
    secondary_service=(terraform_configs["secondary_service_namespace"]["value"], terraform_configs["secondary_service_name"]["value"]),
    ilb_primary_backend_service=terraform_configs["ilb_primary_backend_name"]["value"],
    ilb_secondary_backend_service=terraform_configs["ilb_secondary_backend_name"]["value"],
    ilb_primary_backend_service_parsed=parse_backend_service(terraform_configs["ilb_primary_backend_name"]["value"]),
    ilb_secondary_backend_service_parsed=parse_backend_service(terraform_configs["ilb_secondary_backend_name"]["value"]),
    ilb_primary_alt_backend_service=terraform_configs["ilb_primary_alt_backend_name"]["value"],
    ilb_secondary_alt_backend_service=terraform_configs["ilb_secondary_alt_backend_name"]["value"],
    ilb_primary_alt_backend_service_parsed=parse_backend_service(terraform_configs["ilb_primary_alt_backend_name"]["value"]),
    ilb_secondary_alt_backend_service_parsed=parse_backend_service(terraform_configs["ilb_secondary_alt_backend_name"]["value"]),
    xlb_backend_service=terraform_configs["xlb_backend_name"]["value"],
    xlb_backend_service_parsed=parse_backend_service(terraform_configs["xlb_backend_name"]["value"]),
    loadbalancer_url=terraform_configs["xlb_url"]["value"],
    service_port=int(terraform_configs["service_port"]["value"])
)

credentials, project = google.auth.default()
credentials.refresh(google.auth.transport.requests.Request())

primary_client = get_primary_kubernetes_client(credentials.token, config)
secondary_client = get_secondary_kubernetes_client(credentials.token, config)

original_svc_on_primary = get_service(primary_client, config.primary_service[0], config.primary_service[1])
original_svc_on_secondary = get_service(secondary_client, config.secondary_service[0], config.secondary_service[1])
original_primary_config = original_svc_on_primary.metadata.annotations["controller.autoneg.dev/neg"]
original_secondary_config = original_svc_on_secondary.metadata.annotations["controller.autoneg.dev/neg"]

idx = 1
for test in tests:
    print(f"{str.format('{:>3}', idx)}/{len(tests)} Running test: {test[0]}: ", file=sys.stderr, end="", flush=True)
    try:
        test[1](config, primary_client, secondary_client, 120.0)
    except AssertionError as e:
        print(colored("FAIL", "red"), file=sys.stderr, flush=True)
        traceback.print_exception(e, file=sys.stderr, flush=True)
        break
    except Exception as e:
        print(colored("FAIL", "red"), file=sys.stderr, flush=True)
        traceback.print_exception(e, file=sys.stderr, flush=True)
        break
    print(colored("OK", "green"), file=sys.stderr, flush=True)
    idx += 1

sleep(60)

print(f"Returning services to original state: ", end="", file=sys.stderr, flush=True)
restore_svc_on_primary = get_service(primary_client, config.primary_service[0], config.primary_service[1])
restore_svc_on_secondary = get_service(secondary_client, config.secondary_service[0], config.secondary_service[1])
restore_svc_on_primary.metadata.annotations["controller.autoneg.dev/neg"] = original_primary_config
restore_svc_on_secondary.metadata.annotations["controller.autoneg.dev/neg"] = original_secondary_config
patch_service(primary_client, config.primary_service[0], config.primary_service[1], restore_svc_on_primary)
patch_service(secondary_client, config.secondary_service[0], config.secondary_service[1], restore_svc_on_secondary)
print(colored("OK", "green"), file=sys.stderr, flush=True)

# print(f"Collecting last Autoneg container logs...", file=sys.stderr,)
# autoneg_logs = collect_autoneg_manager_logs("30 minutes", config)
autoneg_logs = {}

for cluster, logs in autoneg_logs.items():
    print(f"Logs from: {cluster}", file=sys.stderr)
    for log in logs:
        print(log)
        if "json_payload" in log:
            print(f"{log['timestamp']} {json.dumps(log['json_payload'])}", file=sys.stderr)
        elif "text_payload" in log:
            print(f"{log['timestamp']} {log['text_payload']}", file=sys.stderr)
        else:
            print(f"{log['timestamp']} {json.dumps(log)}", file=sys.stderr)

    print("", file=sys.stderr)

