output "linode_instance_ip_addresses" {
  value = [
    for idx, instance in linode_instance.rancher : "Linode IP address: ${tolist(instance.ipv4)[0]}"
  ]
}

output "aws_route53_urls" {
  value = [
    for idx, record in aws_route53_record.rancher : "Rancher URL: https://${record.fqdn}"
  ]
}

output "flat_outputs" {
  value = merge(
    {
      for idx, instance in linode_instance.rancher : "linode_${idx}_ip" => tolist(instance.ipv4)[0]
    },
    {
      for idx, record in aws_route53_record.rancher : "linode_${idx}_rancher_url" => "https://${record.fqdn}"
    },
    {
      for idx, item in var.rancher_instances : "linode_${idx + 1}_rancher_version" => item.rancher_version
    }
  )
}
