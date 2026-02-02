# Terraform Deployment for OCI

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
  region = var.region
}

variable "region" {
  description = "OCI region"
  type        = string
  default     = "us-ashburn-1"
}

variable "compartment_id" {
  description = "OCI compartment OCID"
  type        = string
}

variable "team_name" {
  description = "Team name for Vault instance"
  type        = string
}

variable "vault_token" {
  description = "Vault authentication token"
  type        = string
  sensitive   = true
  default     = ""
}

# Generate random token if not provided
resource "random_password" "vault_token" {
  length  = 32
  special = false
}

locals {
  vault_token = var.vault_token != "" ? var.vault_token : "evt_${var.team_name}_${random_password.vault_token.result}"
  vault_url   = "https://vault-${var.team_name}.${var.region}.oci.envector.io"
}

# VCN for Vault
resource "oci_core_vcn" "vault_vcn" {
  compartment_id = var.compartment_id
  display_name   = "vault-${var.team_name}-vcn"
  cidr_block     = "10.0.0.0/16"
  dns_label      = "vaultvcn"
}

# Public subnet
resource "oci_core_subnet" "vault_subnet" {
  compartment_id    = var.compartment_id
  vcn_id            = oci_core_vcn.vault_vcn.id
  display_name      = "vault-${var.team_name}-subnet"
  cidr_block        = "10.0.1.0/24"
  dns_label         = "vaultsub"
  security_list_ids = [oci_core_security_list.vault_security_list.id]
  route_table_id    = oci_core_route_table.vault_route_table.id
}

# Internet Gateway
resource "oci_core_internet_gateway" "vault_ig" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.vault_vcn.id
  display_name   = "vault-${var.team_name}-ig"
}

# Route Table
resource "oci_core_route_table" "vault_route_table" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.vault_vcn.id
  display_name   = "vault-${var.team_name}-rt"

  route_rules {
    destination       = "0.0.0.0/0"
    network_entity_id = oci_core_internet_gateway.vault_ig.id
  }
}

# Security List (Allow HTTPS and SSH)
resource "oci_core_security_list" "vault_security_list" {
  compartment_id = var.compartment_id
  vcn_id         = oci_core_vcn.vault_vcn.id
  display_name   = "vault-${var.team_name}-sl"

  egress_security_rules {
    destination = "0.0.0.0/0"
    protocol    = "all"
  }

  ingress_security_rules {
    protocol = "6" # TCP
    source   = "0.0.0.0/0"

    tcp_options {
      min = 443
      max = 443
    }
  }

  ingress_security_rules {
    protocol = "6" # TCP
    source   = "0.0.0.0/0"

    tcp_options {
      min = 22
      max = 22
    }
  }

  ingress_security_rules {
    protocol = "6" # TCP
    source   = "0.0.0.0/0"

    tcp_options {
      min = 50080
      max = 50080
    }
  }
}

# Compute Instance for Vault
resource "oci_core_instance" "vault_instance" {
  compartment_id      = var.compartment_id
  availability_domain = data.oci_identity_availability_domains.ads.availability_domains[0].name
  display_name        = "vault-${var.team_name}"
  shape               = "VM.Standard.E4.Flex"

  shape_config {
    ocpus         = 1
    memory_in_gbs = 4
  }

  create_vnic_details {
    subnet_id        = oci_core_subnet.vault_subnet.id
    display_name     = "vault-${var.team_name}-vnic"
    assign_public_ip = true
  }

  source_details {
    source_type = "image"
    source_id   = data.oci_core_images.ubuntu_image.images[0].id
  }

  metadata = {
    ssh_authorized_keys = file("~/.ssh/id_rsa.pub")
    user_data = base64encode(templatefile("${path.module}/cloud-init.yaml", {
      vault_token = local.vault_token
      team_name   = var.team_name
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
output "vault_url" {
  value       = local.vault_url
  description = "Rune-Vault URL"
}

output "vault_token" {
  value       = local.vault_token
  description = "Vault authentication token"
  sensitive   = true
}

output "vault_public_ip" {
  value       = oci_core_instance.vault_instance.public_ip
  description = "Public IP of Vault instance"
}

output "ssh_command" {
  value       = "ssh ubuntu@${oci_core_instance.vault_instance.public_ip}"
  description = "SSH command to connect to Vault instance"
}
