resource "random_pet" "random_pet" {

  keepers = {
    aws_prefix = "${var.aws_prefix}"
  }

  length    = 2
  separator = "-"
}

resource "random_pet" "random_pet_rds" {

  keepers = {
    aws_prefix = "${var.aws_prefix}"
  }

  length    = 2
  separator = ""
}

locals {
  aws_lb_name_prefix     = substr(var.aws_prefix, 0, 19)
  aws_lb_name_suffix     = substr(replace(random_pet.random_pet.id, "-", ""), 0, 8)
  aws_tg_80_name         = "${local.aws_lb_name_prefix}-80-${local.aws_lb_name_suffix}"
  aws_tg_443_name        = "${local.aws_lb_name_prefix}-443-${local.aws_lb_name_suffix}"
  aws_load_balancer_name = "${local.aws_lb_name_prefix}-nlb-${local.aws_lb_name_suffix}"
}

data "aws_iam_policy_document" "ec2_assume_role" {
  statement {
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "ssm_role" {
  name               = "${var.aws_prefix}-ssm-${random_pet.random_pet.id}"
  assume_role_policy = data.aws_iam_policy_document.ec2_assume_role.json
  tags               = merge(var.common_tags, { Name = "${var.aws_prefix}-ssm-${random_pet.random_pet.id}" })
}

resource "aws_iam_role_policy_attachment" "ssm_core" {
  role       = aws_iam_role.ssm_role.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_instance_profile" "ssm_profile" {
  name = "${var.aws_prefix}-ssm-${random_pet.random_pet.id}"
  role = aws_iam_role.ssm_role.name
  tags = merge(var.common_tags, { Name = "${var.aws_prefix}-ssm-${random_pet.random_pet.id}" })
}

resource "aws_instance" "aws_instance" {
  count                  = 2
  ami                    = var.aws_ami
  instance_type          = var.aws_ec2_instance_type
  subnet_id              = var.aws_subnet_id
  vpc_security_group_ids = [var.aws_security_group_id]
  iam_instance_profile   = aws_iam_instance_profile.ssm_profile.name
  key_name               = var.aws_pem_key_name != "" ? var.aws_pem_key_name : null
  user_data              = <<-EOF
    #!/bin/bash
    set -euxo pipefail

    if systemctl list-unit-files | grep -q '^amazon-ssm-agent.service'; then
      systemctl enable amazon-ssm-agent
      systemctl restart amazon-ssm-agent
      exit 0
    fi

    if systemctl list-unit-files | grep -q '^snap.amazon-ssm-agent.amazon-ssm-agent.service'; then
      systemctl enable snap.amazon-ssm-agent.amazon-ssm-agent.service
      systemctl restart snap.amazon-ssm-agent.amazon-ssm-agent.service || snap start amazon-ssm-agent
      exit 0
    fi

    if command -v snap >/dev/null 2>&1; then
      snap install amazon-ssm-agent --classic
      snap start amazon-ssm-agent
      exit 0
    fi
  EOF

  root_block_device {
    volume_size = 200
    tags = merge(var.common_tags, {
      Name = "${random_pet.random_pet.keepers.aws_prefix}-${random_pet.random_pet.id}"
    })
  }

  tags = merge(var.common_tags, {
    Name = "${random_pet.random_pet.keepers.aws_prefix}-${random_pet.random_pet.id}"
  })
}

resource "aws_lb_target_group" "aws_lb_target_group_80" {
  name        = local.aws_tg_80_name
  port        = 80
  protocol    = "HTTP"
  target_type = "instance"
  vpc_id      = var.aws_vpc
  tags        = merge(var.common_tags, { Name = "${var.aws_prefix}-80-${random_pet.random_pet.id}" })

  health_check {
    protocol          = "HTTP"
    port              = "traffic-port"
    healthy_threshold = 3
    interval          = 10
  }
}

resource "aws_lb_target_group" "aws_lb_target_group_443" {
  name        = local.aws_tg_443_name
  port        = 443
  protocol    = "HTTPS"
  target_type = "instance"
  vpc_id      = var.aws_vpc
  tags        = merge(var.common_tags, { Name = "${var.aws_prefix}-443-${random_pet.random_pet.id}" })

  health_check {
    protocol          = "HTTPS"
    port              = 443
    healthy_threshold = 3
    interval          = 10
  }
}

# attach instances to the target group 80
resource "aws_lb_target_group_attachment" "attach_tg_80" {
  count            = length(aws_instance.aws_instance)
  target_group_arn = aws_lb_target_group.aws_lb_target_group_80.arn
  target_id        = aws_instance.aws_instance[count.index].id
  port             = 80
}

# attach instances to the target group 443
resource "aws_lb_target_group_attachment" "attach_tg_443" {
  count            = length(aws_instance.aws_instance)
  target_group_arn = aws_lb_target_group.aws_lb_target_group_443.arn
  target_id        = aws_instance.aws_instance[count.index].id
  port             = 443
}

# create a load balancer
resource "aws_lb" "aws_lb" {
  load_balancer_type = "application"
  name               = local.aws_load_balancer_name
  internal           = false
  subnets            = [var.aws_subnet_a, var.aws_subnet_b, var.aws_subnet_c]
  tags               = merge(var.common_tags, { Name = "${var.aws_prefix}-nlb-${random_pet.random_pet.id}" })
}

# add a listener for port 80
resource "aws_lb_listener" "aws_lb_listener_80" {
  load_balancer_arn = aws_lb.aws_lb.arn
  port              = "80"
  protocol          = "HTTP"
  tags              = merge(var.common_tags, { Name = "${var.aws_prefix}-80-listener" })

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.aws_lb_target_group_80.arn
  }
}

resource "aws_rds_cluster" "aws_rds_cluster" {
  cluster_identifier = "${var.aws_prefix}-${random_pet.random_pet_rds.id}"
  engine             = "aurora-mysql"

  # Upgraded to Aurora MySQL 3 (8.0 compatible) for 2026
  engine_version = "8.0.mysql_aurora.3.12.0"

  # Keeps things moving if a previous QA environment wasn't fully cleaned up
  allow_major_version_upgrade = true

  availability_zones      = ["us-east-2a", "us-east-2b", "us-east-2c"]
  database_name           = "db${random_pet.random_pet_rds.id}"
  master_username         = "tfadmin"
  master_password         = var.aws_rds_password
  backup_retention_period = 5
  preferred_backup_window = "07:00-09:00"
  skip_final_snapshot     = true
  tags                    = merge(var.common_tags, { Name = "${var.aws_prefix}-${random_pet.random_pet_rds.id}" })
}

resource "aws_rds_cluster_instance" "aws_rds_cluster_instance" {
  count              = 1
  identifier         = "${var.aws_prefix}-${random_pet.random_pet_rds.id}-${count.index}"
  cluster_identifier = aws_rds_cluster.aws_rds_cluster.id
  instance_class     = "db.r5.large" # Price Per Hour $0.2500
  engine             = aws_rds_cluster.aws_rds_cluster.engine
  engine_version     = aws_rds_cluster.aws_rds_cluster.engine_version
  tags               = merge(var.common_tags, { Name = "${var.aws_prefix}-${random_pet.random_pet_rds.id}-${count.index}" })
}

resource "aws_route53_record" "aws_route53_record" {
  zone_id = data.aws_route53_zone.zone.zone_id
  name    = "${var.aws_prefix}-${random_pet.random_pet.id}"
  type    = "CNAME"
  ttl     = "60"
  records = [aws_lb.aws_lb.dns_name]
}


data "aws_route53_zone" "zone" {
  name = var.aws_route53_fqdn
}

resource "aws_acm_certificate" "cert" {
  domain_name       = "${var.aws_prefix}-${random_pet.random_pet.id}.${var.aws_route53_fqdn}"
  validation_method = "DNS"
  tags              = merge(var.common_tags, { Name = "${var.aws_prefix}-${random_pet.random_pet.id}.${var.aws_route53_fqdn}" })

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_route53_record" "cert_validation" {
  count   = 1
  name    = element(aws_acm_certificate.cert.domain_validation_options.*.resource_record_name, count.index)
  type    = element(aws_acm_certificate.cert.domain_validation_options.*.resource_record_type, count.index)
  zone_id = data.aws_route53_zone.zone.zone_id
  records = [element(aws_acm_certificate.cert.domain_validation_options.*.resource_record_value, count.index)]
  ttl     = 60
}

resource "aws_acm_certificate_validation" "cert" {
  certificate_arn         = aws_acm_certificate.cert.arn
  validation_record_fqdns = aws_route53_record.cert_validation[*].fqdn
}

# update listener to use new certificate
resource "aws_lb_listener" "aws_lb_listener_443" {
  load_balancer_arn = aws_lb.aws_lb.arn
  port              = 443
  protocol          = "HTTPS"
  ssl_policy        = "ELBSecurityPolicy-2016-08"
  certificate_arn   = aws_acm_certificate_validation.cert.certificate_arn
  tags              = merge(var.common_tags, { Name = "${var.aws_prefix}-443-listener" })

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.aws_lb_target_group_443.arn
  }
}
