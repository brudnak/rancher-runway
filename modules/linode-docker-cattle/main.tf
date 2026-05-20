terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.53.0"
    }
    linode = {
      source  = "linode/linode"
      version = ">= 2.28.0"
    }
    random = {
      source  = "hashicorp/random"
      version = ">= 3.6.0"
    }
  }
}

provider "aws" {
  region = var.aws_region
}

provider "linode" {
  token = var.linode_access_token
}

resource "random_pet" "rancher" {
  for_each = {
    for idx, instance in var.rancher_instances : tostring(idx + 1) => instance
  }

  prefix = var.label_prefix
  length = 2
}

resource "linode_instance" "rancher" {
  for_each = random_pet.rancher

  label     = each.value.id
  image     = var.linode_image
  region    = var.linode_region
  type      = var.linode_type
  root_pass = var.linode_ssh_root_password
  tags      = var.linode_tags

  connection {
    type     = "ssh"
    user     = "root"
    password = var.linode_ssh_root_password
    host     = tolist(self.ipv4)[0]
  }

  provisioner "remote-exec" {
    inline = [
      "sudo apt-get update",
      "sudo curl -fsSL https://releases.rancher.com/install-docker/${var.docker_install_version}.sh | sh",
      "docker run -d --name rancher --restart=unless-stopped -p 80:80 -p 443:443 --privileged -e CATTLE_BOOTSTRAP_PASSWORD='${var.rancher_bootstrap_password}' ${var.dockerhub}:${var.rancher_instances[tonumber(each.key) - 1].rancher_version} --acme-domain ${each.value.id}.${var.aws_route53_fqdn}",
    ]
  }
}

data "aws_route53_zone" "zone" {
  name = var.aws_route53_fqdn
}

resource "aws_route53_record" "rancher" {
  for_each = random_pet.rancher

  zone_id = data.aws_route53_zone.zone.zone_id
  name    = each.value.id
  type    = "A"
  ttl     = 60
  records = [tolist(linode_instance.rancher[each.key].ipv4)[0]]
}
