# Terraform Deployment for CI Runner (OCI)
#
# Provisions a self-hosted GitHub Actions runner for Rune-Vault CI.
# Runner labels: self-hosted, vault-ci
#
# Usage:
#   cd deployment/ci/oci
#   terraform init
#   terraform plan -var-file=ci.tfvars
#   terraform apply -var-file=ci.tfvars

terraform {
  required_version = ">= 1.0"
  required_providers {
    oci = {
      source  = "oracle/oci"
      version = "~> 5.0"
    }
  }
}

provider "oci" {
  region              = var.region
  config_file_profile = var.oci_profile
}

variable "oci_profile" {
  description = "OCI CLI config profile name (~/.oci/config)"
  type        = string
  default     = "DEFAULT"
}

variable "region" {
  description = "OCI region"
  type        = string
  default     = "ap-seoul-1"
}

variable "compartment_id" {
  description = "OCI compartment OCID"
  type        = string
}

variable "github_repo" {
  description = "GitHub repository (owner/name)"
  type        = string
  default     = "CryptoLabInc/rune-admin"
}

variable "github_runner_token" {
  description = "GitHub Actions runner registration token (from Settings > Actions > Runners)"
  type        = string
  sensitive   = true
}

variable "runner_labels" {
  description = "Comma-separated runner labels"
  type        = string
  default     = "vault-ci"
}


# VCN for CI Runner
resource "oci_core_vcn" "ci_vcn" {
  compartment_id = var.compartment_id
  display_name   = "vault-ci-vcn"
  cidr_block     = "10.1.0.0/16"
  dns_label      = "civcn"
}

# Public subnet
resource "oci_core_subnet" "ci_subnet" {
  compartment_id    = var.compartment_id
  vcn_id            = oci_core_vcn.ci_vcn.id
  display_name      = "vault-ci-subnet"
  cidr_block        = "10.1.1.0/24"
  dns_label         = "cisub"
  security_list_ids = [oci_core_security_list.ci_security_list.id]
  route_table_id    = oci_core_route_table.ci_route_table.id
}

# Internet Gateway
resource "oci_core_internet_gateway" "ci_ig" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.ci_vcn.id
  display_name   = "vault-ci-ig"
}

# Route Table
resource "oci_core_route_table" "ci_route_table" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.ci_vcn.id
  display_name   = "vault-ci-rt"

  route_rules {
    destination       = "0.0.0.0/0"
    network_entity_id = oci_core_internet_gateway.ci_ig.id
  }
}

# Security List — egress only (no SSH ingress; use OCI Cloud Shell for access)
resource "oci_core_security_list" "ci_security_list" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.ci_vcn.id
  display_name   = "vault-ci-sl"

  egress_security_rules {
    destination = "0.0.0.0/0"
    protocol    = "all"
  }
}

# Compute Instance for CI Runner
resource "oci_core_instance" "ci_runner" {
  compartment_id      = var.compartment_id
  availability_domain = data.oci_identity_availability_domains.ads.availability_domains[0].name
  display_name        = "vault-ci-runner"
  shape               = "VM.Standard.E5.Flex"

  shape_config {
    ocpus         = 2
    memory_in_gbs = 8
  }

  create_vnic_details {
    subnet_id        = oci_core_subnet.ci_subnet.id
    display_name     = "vault-ci-vnic"
    assign_public_ip = true
  }

  source_details {
    source_type = "image"
    source_id   = data.oci_core_images.ubuntu_image.images[0].id
  }

  metadata = {
    user_data = base64encode(templatefile("${path.module}/startup-script.sh", {
      github_repo         = var.github_repo
      github_runner_token = var.github_runner_token
      runner_labels       = var.runner_labels
    }))
  }
}

# Data sources
data "oci_identity_availability_domains" "ads" {
  compartment_id = var.compartment_id
}

data "oci_core_images" "ubuntu_image" {
  compartment_id   = var.compartment_id
  operating_system = "Canonical Ubuntu"
  sort_by          = "TIMECREATED"
  sort_order       = "DESC"

  filter {
    name   = "display_name"
    values = ["^Canonical-Ubuntu-22.04-.*"]
    regex  = true
  }
}

# Outputs
output "runner_public_ip" {
  value       = oci_core_instance.ci_runner.public_ip
  description = "Public IP of CI runner instance"
}

output "console_url" {
  value       = "https://cloud.oracle.com/compute/instances/${oci_core_instance.ci_runner.id}?region=${var.region}"
  description = "OCI Console URL for CI runner instance (use Cloud Shell for access)"
}
