# modules/rke2-ha/main.tf

# Variables
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

variable "server_count" {
  type        = number
  description = "Number of RKE2 server nodes for this Rancher cluster. Use 1 for a single-server install, or an odd HA count such as 3 or 5."
  default     = 3

  validation {
    condition     = contains([1, 3, 5], var.server_count)
    error_message = "server_count must be 1, 3, or 5."
  }
}

variable "aws_route53_fqdn" {
  type        = string
  description = "Route53 FQDN for DNS records"
}

variable "custom_hostname_prefix" {
  type        = string
  description = "Optional custom DNS label for the Rancher URL. Resource names still use aws_prefix."
  default     = ""
}

variable "common_tags" {
  type        = map(string)
  description = "Common ownership and run tags applied to taggable AWS resources."
}

variable "gpu_worker_enabled" {
  type        = bool
  description = "Whether to add one worker-only GPU node to this HA cluster."
  default     = false
}

variable "gpu_worker_instance_type" {
  type        = string
  description = "EC2 instance type for the optional worker-only GPU node."
  default     = "g5.xlarge"
}

variable "gpu_worker_ami" {
  type        = string
  description = "AMI ID for the optional worker-only GPU node."
  default     = ""
}

variable "gpu_worker_subnet_id" {
  type        = string
  description = "Subnet ID for the optional worker-only GPU node."
  default     = ""
}

locals {
  resource_name_prefix = var.aws_prefix
  dns_label            = trimspace(var.custom_hostname_prefix) != "" ? trimspace(var.custom_hostname_prefix) : local.resource_name_prefix
  target_group_prefix  = substr(local.resource_name_prefix, 0, 28)
  domain_name          = "${local.dns_label}.${var.aws_route53_fqdn}"
}

resource "aws_instance" "aws_instance" {
  count                  = var.server_count
  ami                    = var.aws_ami
  instance_type          = "t3a.large"
  subnet_id              = var.aws_subnet_id
  vpc_security_group_ids = [var.aws_security_group_id]
  key_name               = var.aws_pem_key_name
  iam_instance_profile   = aws_iam_instance_profile.ssm_profile.name

  root_block_device {
    volume_size = 200
    tags = merge(var.common_tags, {
      Name = "${local.resource_name_prefix}-${count.index + 1}"
    })
  }

  tags = merge(var.common_tags, {
    Name = "${local.resource_name_prefix}-${count.index + 1}"
  })
}

resource "aws_instance" "gpu_worker" {
  count                  = var.gpu_worker_enabled ? 1 : 0
  ami                    = trimspace(var.gpu_worker_ami) != "" ? trimspace(var.gpu_worker_ami) : var.aws_ami
  instance_type          = var.gpu_worker_instance_type
  subnet_id              = trimspace(var.gpu_worker_subnet_id) != "" ? trimspace(var.gpu_worker_subnet_id) : var.aws_subnet_id
  vpc_security_group_ids = [var.aws_security_group_id]
  key_name               = var.aws_pem_key_name
  iam_instance_profile   = aws_iam_instance_profile.ssm_profile.name

  root_block_device {
    volume_size = 200
    tags = merge(var.common_tags, {
      Name                         = "${local.resource_name_prefix}-gpu-worker"
      HA_Rancher_RKE2_GPU_Worker   = "true"
      HA_Rancher_RKE2_GPU_Purpose  = "rancher-ai-liz"
      HA_Rancher_RKE2_GPU_Warning  = "do-not-leave-running-unused"
      HA_Rancher_RKE2_GPU_Instance = var.gpu_worker_instance_type
    })
  }

  tags = merge(var.common_tags, {
    Name                         = "${local.resource_name_prefix}-gpu-worker"
    Role                         = "rke2-gpu-worker"
    HA_Rancher_RKE2_GPU_Worker   = "true"
    HA_Rancher_RKE2_GPU_Purpose  = "rancher-ai-liz"
    HA_Rancher_RKE2_GPU_Warning  = "do-not-leave-running-unused"
    HA_Rancher_RKE2_GPU_Instance = var.gpu_worker_instance_type
    HA_Rancher_RKE2_GPU_AMI      = trimspace(var.gpu_worker_ami) != "" ? trimspace(var.gpu_worker_ami) : var.aws_ami
  })

  timeouts {
    create = "6m"
  }
}

# Application Load Balancer for Rancher UI. Public TLS terminates at the ALB,
# then forwards to Rancher's HTTP ingress because Helm uses tls=external.
resource "aws_lb_target_group" "aws_lb_target_group_80" {
  name        = "${local.target_group_prefix}-80"
  port        = 80
  protocol    = "HTTP"
  target_type = "instance"
  vpc_id      = var.aws_vpc
  tags        = merge(var.common_tags, { Name = "${local.resource_name_prefix}-80" })

  health_check {
    protocol          = "HTTP"
    port              = "traffic-port"
    healthy_threshold = 3
    interval          = 10
  }
}

resource "aws_lb_target_group_attachment" "attach_tg_80" {
  count            = length(aws_instance.aws_instance)
  target_group_arn = aws_lb_target_group.aws_lb_target_group_80.arn
  target_id        = aws_instance.aws_instance[count.index].id
  port             = 80
}

resource "aws_lb" "aws_lb" {
  load_balancer_type = "application"
  name               = local.resource_name_prefix
  internal           = false
  ip_address_type    = "ipv4"
  subnets            = [var.aws_subnet_a, var.aws_subnet_b, var.aws_subnet_c]
  tags               = merge(var.common_tags, { Name = local.resource_name_prefix })
}

resource "aws_lb_listener" "aws_lb_listener_80" {
  load_balancer_arn = aws_lb.aws_lb.arn
  port              = "80"
  protocol          = "HTTP"
  tags              = merge(var.common_tags, { Name = "${local.resource_name_prefix}-80-listener" })

  default_action {
    type = "redirect"
    redirect {
      port        = "443"
      protocol    = "HTTPS"
      status_code = "HTTP_301"
    }
  }
}

# Route53 and ACM Certificate configuration
data "aws_route53_zone" "zone" {
  name = var.aws_route53_fqdn
}

resource "aws_route53_record" "aws_route53_record" {
  zone_id = data.aws_route53_zone.zone.zone_id
  name    = local.dns_label
  type    = "CNAME"
  ttl     = "60"
  records = [aws_lb.aws_lb.dns_name]
}

resource "aws_acm_certificate" "cert" {
  domain_name       = local.domain_name
  validation_method = "DNS"
  tags              = merge(var.common_tags, { Name = local.domain_name })

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_route53_record" "cert_validation" {
  count = 1

  name    = tolist(aws_acm_certificate.cert.domain_validation_options)[count.index].resource_record_name
  type    = tolist(aws_acm_certificate.cert.domain_validation_options)[count.index].resource_record_type
  zone_id = data.aws_route53_zone.zone.zone_id
  records = [tolist(aws_acm_certificate.cert.domain_validation_options)[count.index].resource_record_value]
  ttl     = 60
}

resource "aws_acm_certificate_validation" "cert" {
  certificate_arn         = aws_acm_certificate.cert.arn
  validation_record_fqdns = aws_route53_record.cert_validation[*].fqdn
}

resource "aws_lb_listener" "aws_lb_listener_443" {
  load_balancer_arn = aws_lb.aws_lb.arn
  port              = "443"
  protocol          = "HTTPS"
  ssl_policy        = "ELBSecurityPolicy-2016-08"
  certificate_arn   = aws_acm_certificate_validation.cert.certificate_arn
  tags              = merge(var.common_tags, { Name = "${local.resource_name_prefix}-443-listener" })

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.aws_lb_target_group_80.arn
  }
}

# Outputs
output "server_ips" {
  value = aws_instance.aws_instance[*].public_ip
}

output "server_private_ips" {
  value = aws_instance.aws_instance[*].private_ip
}

output "server_count" {
  value = var.server_count
}

output "server1_ip" {
  value = aws_instance.aws_instance[0].public_ip
}

output "server2_ip" {
  value = try(aws_instance.aws_instance[1].public_ip, "")
}

output "server3_ip" {
  value = try(aws_instance.aws_instance[2].public_ip, "")
}

output "server4_ip" {
  value = try(aws_instance.aws_instance[3].public_ip, "")
}

output "server5_ip" {
  value = try(aws_instance.aws_instance[4].public_ip, "")
}

output "server1_private_ip" {
  value = aws_instance.aws_instance[0].private_ip
}

output "server2_private_ip" {
  value = try(aws_instance.aws_instance[1].private_ip, "")
}

output "server3_private_ip" {
  value = try(aws_instance.aws_instance[2].private_ip, "")
}

output "server4_private_ip" {
  value = try(aws_instance.aws_instance[3].private_ip, "")
}

output "server5_private_ip" {
  value = try(aws_instance.aws_instance[4].private_ip, "")
}

output "gpu_worker_ip" {
  value = var.gpu_worker_enabled ? aws_instance.gpu_worker[0].public_ip : ""
}

output "gpu_worker_private_ip" {
  value = var.gpu_worker_enabled ? aws_instance.gpu_worker[0].private_ip : ""
}

output "gpu_worker_instance_type" {
  value = var.gpu_worker_enabled ? var.gpu_worker_instance_type : ""
}

output "gpu_worker_ami" {
  value = var.gpu_worker_enabled ? (trimspace(var.gpu_worker_ami) != "" ? trimspace(var.gpu_worker_ami) : var.aws_ami) : ""
}

output "gpu_worker_subnet_id" {
  value = var.gpu_worker_enabled ? (trimspace(var.gpu_worker_subnet_id) != "" ? trimspace(var.gpu_worker_subnet_id) : var.aws_subnet_id) : ""
}

output "aws_lb" {
  value = aws_lb.aws_lb.dns_name
}

output "rancher_url" {
  value = local.domain_name
}
