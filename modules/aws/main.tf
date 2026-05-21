# main.tf in root directory
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "6.40.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "3.7.2"
    }
  }
}

# Variables
variable "total_has" {
  type        = number
  description = "Number of HA instances to create"
  default     = 1
}

variable "deployment_type" {
  type        = string
  description = "Deployment shape to create: ha-rke2 or hosted-tenant-k3s."
  default     = "ha-rke2"

  validation {
    condition     = contains(["ha-rke2", "hosted-tenant-k3s"], var.deployment_type)
    error_message = "deployment_type must be ha-rke2 or hosted-tenant-k3s."
  }
}

variable "total_rancher_instances" {
  type        = number
  description = "Total hosted/tenant Rancher instances to create, including the host at index 1."
  default     = 0

  validation {
    condition     = var.total_rancher_instances == 0 || (var.total_rancher_instances >= 2 && var.total_rancher_instances <= 4)
    error_message = "total_rancher_instances must be 0 or between 2 and 4."
  }
}

variable "aws_region" {
  type        = string
  description = "AWS region"
  default     = "us-east-2"
}

variable "aws_prefix" {
  type        = string
  description = "Prefix for resource names"
}

variable "aws_vpc" {
  type        = string
  description = "VPC ID"
}

variable "aws_subnet_a" {
  type        = string
  description = "Subnet A ID"
}

variable "aws_subnet_b" {
  type        = string
  description = "Subnet B ID"
}

variable "aws_subnet_c" {
  type        = string
  description = "Subnet C ID"
}

variable "aws_ami" {
  type        = string
  description = "AMI ID for instances"
}

variable "aws_subnet_id" {
  type        = string
  description = "Subnet ID for instances"
}

variable "aws_security_group_id" {
  type        = string
  description = "Security group ID"
}

variable "aws_pem_key_name" {
  type        = string
  description = "Name of the PEM key for SSH access"
}

variable "aws_rds_password" {
  type        = string
  description = "RDS password for hosted-tenant K3s datastore clusters."
  default     = ""
  sensitive   = true

  validation {
    condition     = var.aws_rds_password == "" || (length(var.aws_rds_password) >= 8 && length(var.aws_rds_password) <= 41 && length(regexall("[/'\"@ ]", var.aws_rds_password)) == 0)
    error_message = "aws_rds_password must be 8-41 characters and cannot contain /, ', \", @, or spaces."
  }
}

variable "aws_ec2_instance_type" {
  type        = string
  description = "EC2 instance type for hosted-tenant K3s nodes."
  default     = "m5.large"
}

variable "server_count" {
  type        = number
  description = "Number of RKE2 server nodes per Rancher cluster. Use 1 for a single-server install, or 3/5 for odd-sized HA."
  default     = 3

  validation {
    condition     = contains([1, 3, 5], var.server_count)
    error_message = "server_count must be 1, 3, or 5."
  }
}

variable "gpu_worker_enabled" {
  type        = bool
  description = "Whether each HA RKE2 cluster should include one worker-only GPU node for Rancher AI testing."
  default     = false
}

variable "gpu_worker_instance_type" {
  type        = string
  description = "EC2 instance type for optional HA RKE2 GPU worker nodes."
  default     = "g5.xlarge"
}

variable "gpu_worker_ami" {
  type        = string
  description = "Optional AMI override for GPU worker nodes. When empty, the module discovers the latest matching AWS Deep Learning Base GPU AMI."
  default     = ""
}

variable "gpu_worker_ami_name_filter" {
  type        = string
  description = "AMI name filter used when discovering the GPU worker AMI."
  default     = "Deep Learning Base OSS Nvidia Driver GPU AMI (Ubuntu 22.04)*"
}

variable "gpu_worker_subnet_id" {
  type        = string
  description = "Optional subnet override for GPU worker nodes. When empty, worker nodes are spread across the configured HA subnets."
  default     = ""
}

variable "aws_route53_fqdn" {
  type        = string
  description = "Route53 FQDN for DNS records"
}

variable "custom_hostname_prefix" {
  type        = string
  description = "Optional custom Rancher DNS label. When set, total_has must be 1."
  default     = ""
}

variable "owner_first_name" {
  type        = string
  description = "First name of the person responsible for this run."
}

variable "owner_last_name" {
  type        = string
  description = "Last name of the person responsible for this run."
}

variable "run_id" {
  type        = string
  description = "Control panel run id used to find and audit resources."
  default     = ""
}

# Module configuration
locals {
  normalized_deployment_type      = trimspace(var.deployment_type) != "" ? trimspace(var.deployment_type) : "ha-rke2"
  hosted_tenant_total_instances   = var.total_rancher_instances > 0 ? var.total_rancher_instances : var.total_has
  ha_instances                    = local.normalized_deployment_type == "ha-rke2" ? { for i in range(1, var.total_has + 1) : i => "${var.aws_prefix}-h${i}" } : {}
  hosted_tenant_rancher_instances = local.normalized_deployment_type == "hosted-tenant-k3s" ? { for i in range(1, local.hosted_tenant_total_instances + 1) : i => "${var.aws_prefix}-t${i}" } : {}
  gpu_worker_enabled_for_ha       = local.normalized_deployment_type == "ha-rke2" && var.gpu_worker_enabled
  default_gpu_worker_subnet_ids   = [var.aws_subnet_b, var.aws_subnet_c, var.aws_subnet_id]
  resolved_gpu_worker_ami         = local.gpu_worker_enabled_for_ha ? (trimspace(var.gpu_worker_ami) != "" ? trimspace(var.gpu_worker_ami) : data.aws_ami.gpu_worker[0].id) : ""
  owner_name                      = trimspace("${trimspace(var.owner_first_name)} ${trimspace(var.owner_last_name)}")
  common_tags = {
    NamePrefix             = var.aws_prefix
    Owner                  = local.owner_name
    ManagedBy              = "rancher-runway"
    HA_Rancher_RKE2_Run_ID = trimspace(var.run_id) != "" ? trimspace(var.run_id) : var.aws_prefix
  }
}

data "aws_ami" "gpu_worker" {
  count       = local.gpu_worker_enabled_for_ha && trimspace(var.gpu_worker_ami) == "" ? 1 : 0
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = [var.gpu_worker_ami_name_filter]
  }

  filter {
    name   = "state"
    values = ["available"]
  }
}

provider "aws" {
  region = var.aws_region

  default_tags {
    tags = local.common_tags
  }
}

module "ha" {
  for_each = local.ha_instances
  source   = "./modules/rke2-ha"

  aws_prefix               = each.value
  aws_vpc                  = var.aws_vpc
  aws_subnet_a             = var.aws_subnet_a
  aws_subnet_b             = var.aws_subnet_b
  aws_subnet_c             = var.aws_subnet_c
  aws_ami                  = var.aws_ami
  aws_subnet_id            = var.aws_subnet_id
  aws_security_group_id    = var.aws_security_group_id
  aws_pem_key_name         = var.aws_pem_key_name
  server_count             = var.server_count
  aws_route53_fqdn         = var.aws_route53_fqdn
  custom_hostname_prefix   = trimspace(var.custom_hostname_prefix)
  gpu_worker_enabled       = local.gpu_worker_enabled_for_ha
  gpu_worker_instance_type = var.gpu_worker_instance_type
  gpu_worker_ami           = local.resolved_gpu_worker_ami
  gpu_worker_subnet_id     = trimspace(var.gpu_worker_subnet_id) != "" ? trimspace(var.gpu_worker_subnet_id) : local.default_gpu_worker_subnet_ids[(tonumber(each.key) - 1) % length(local.default_gpu_worker_subnet_ids)]
  common_tags              = local.common_tags
}

module "hosted_tenant" {
  for_each = local.hosted_tenant_rancher_instances
  source   = "./modules/k3s-tenant-ha"

  aws_prefix            = each.value
  aws_vpc               = var.aws_vpc
  aws_subnet_a          = var.aws_subnet_a
  aws_subnet_b          = var.aws_subnet_b
  aws_subnet_c          = var.aws_subnet_c
  aws_ami               = var.aws_ami
  aws_subnet_id         = var.aws_subnet_id
  aws_security_group_id = var.aws_security_group_id
  aws_pem_key_name      = var.aws_pem_key_name
  aws_rds_password      = var.aws_rds_password
  aws_route53_fqdn      = var.aws_route53_fqdn
  aws_ec2_instance_type = var.aws_ec2_instance_type
  common_tags           = local.common_tags
}

# Outputs
output "ha_details" {
  value = {
    for idx, instance in module.ha : "ha_${idx}" => {
      server_count             = instance.server_count
      server_ips               = instance.server_ips
      server_private_ips       = instance.server_private_ips
      server1_ip               = instance.server1_ip
      server2_ip               = instance.server2_ip
      server3_ip               = instance.server3_ip
      server4_ip               = instance.server4_ip
      server5_ip               = instance.server5_ip
      server1_private_ip       = instance.server1_private_ip
      server2_private_ip       = instance.server2_private_ip
      server3_private_ip       = instance.server3_private_ip
      server4_private_ip       = instance.server4_private_ip
      server5_private_ip       = instance.server5_private_ip
      aws_lb                   = instance.aws_lb
      rancher_url              = instance.rancher_url
      gpu_worker_ip            = instance.gpu_worker_ip
      gpu_worker_private_ip    = instance.gpu_worker_private_ip
      gpu_worker_instance_type = instance.gpu_worker_instance_type
      gpu_worker_ami           = instance.gpu_worker_ami
      gpu_worker_subnet_id     = instance.gpu_worker_subnet_id
    }
  }
  sensitive = true
}

output "hosted_tenant_details" {
  value = {
    for idx, instance in module.hosted_tenant : "hosted_${idx}" => {
      server1_ip     = instance.server1_ip
      server2_ip     = instance.server2_ip
      mysql_endpoint = instance.mysql_endpoint
      mysql_password = instance.mysql_password
      aws_lb         = instance.aws_lb
      rancher_url    = instance.rancher_url
    }
  }
  sensitive = true
}

output "flat_outputs" {
  value = merge(concat([
    for idx, instance in module.ha : {
      "ha_${idx}_server_count"             = tostring(instance.server_count)
      "ha_${idx}_server_ips"               = join(",", instance.server_ips)
      "ha_${idx}_server_private_ips"       = join(",", instance.server_private_ips)
      "ha_${idx}_server1_ip"               = instance.server1_ip
      "ha_${idx}_server2_ip"               = instance.server2_ip
      "ha_${idx}_server3_ip"               = instance.server3_ip
      "ha_${idx}_server4_ip"               = instance.server4_ip
      "ha_${idx}_server5_ip"               = instance.server5_ip
      "ha_${idx}_server1_private_ip"       = instance.server1_private_ip
      "ha_${idx}_server2_private_ip"       = instance.server2_private_ip
      "ha_${idx}_server3_private_ip"       = instance.server3_private_ip
      "ha_${idx}_server4_private_ip"       = instance.server4_private_ip
      "ha_${idx}_server5_private_ip"       = instance.server5_private_ip
      "ha_${idx}_aws_lb"                   = instance.aws_lb
      "ha_${idx}_rancher_url"              = instance.rancher_url
      "ha_${idx}_gpu_worker_ip"            = instance.gpu_worker_ip
      "ha_${idx}_gpu_worker_private_ip"    = instance.gpu_worker_private_ip
      "ha_${idx}_gpu_worker_instance_type" = instance.gpu_worker_instance_type
      "ha_${idx}_gpu_worker_ami"           = instance.gpu_worker_ami
      "ha_${idx}_gpu_worker_subnet_id"     = instance.gpu_worker_subnet_id
    }
    ], [
    for idx, instance in module.hosted_tenant : {
      "hosted_${idx}_server1_ip"     = instance.server1_ip
      "hosted_${idx}_server2_ip"     = instance.server2_ip
      "hosted_${idx}_mysql_endpoint" = instance.mysql_endpoint
      "hosted_${idx}_mysql_password" = instance.mysql_password
      "hosted_${idx}_aws_lb"         = instance.aws_lb
      "hosted_${idx}_rancher_url"    = instance.rancher_url
    }
  ])...)
  sensitive = true
}
