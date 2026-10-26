#!/bin/sh
terraform import zerops_service.app '<service_id>'
# Alternatively:
terraform import zerops_service.app '<project_id>/app'
# Import sets type to its canonical base and leaves desired_state unmanaged.
