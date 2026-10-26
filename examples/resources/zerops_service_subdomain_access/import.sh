#!/usr/bin/env bash
# Access must already be enabled; import uses the service ID, not a route ID.
terraform import zerops_service_subdomain_access.app "$SERVICE_ID"
