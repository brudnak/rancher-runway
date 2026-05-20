variable "aws_region" {
  description = "AWS region used for Route53 API calls."
  type        = string
  default     = "us-east-2"
}

variable "aws_route53_fqdn" {
  description = "Route53 hosted zone name used for Rancher DNS records."
  type        = string
}

variable "label_prefix" {
  description = "Prefix for generated Linode labels and Route53 names."
  type        = string
}

variable "linode_access_token" {
  description = "Linode API token."
  type        = string
  sensitive   = true
}

variable "linode_ssh_root_password" {
  description = "Root SSH password used for initial Docker installation."
  type        = string
  sensitive   = true
}

variable "linode_region" {
  description = "Linode region."
  type        = string
  default     = "us-west"
}

variable "linode_type" {
  description = "Linode instance type."
  type        = string
  default     = "g6-standard-6"
}

variable "linode_image" {
  description = "Linode image."
  type        = string
  default     = "linode/ubuntu22.04"
}

variable "linode_tags" {
  description = "Tags to add to Linode instances."
  type        = list(string)
  default     = []
}

variable "rancher_bootstrap_password" {
  description = "Bootstrap password for Rancher."
  type        = string
  sensitive   = true
}

variable "dockerhub" {
  description = "Rancher image repository."
  type        = string
  default     = "rancher/rancher"
}

variable "docker_install_version" {
  description = "Rancher Docker install script version."
  type        = string
  default     = "27.1"
}

variable "rancher_instances" {
  description = "Docker Rancher instances to create."
  type = list(object({
    rancher_version = string
  }))
}
