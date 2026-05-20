variable "aws_prefix" {
  type        = string
  description = "The prefix for the resources."
}

variable "aws_route53_fqdn" {
  type        = string
  description = "The fully qualified domain name to use."
}

variable "aws_vpc" {
  type        = string
  description = "The VPC to use."
}

variable "aws_subnet_a" {
  type        = string
  description = "The subnet A to use."
}

variable "aws_subnet_b" {
  type        = string
  description = "The subnet B to use."
}

variable "aws_subnet_c" {
  type        = string
  description = "The subnet C to use."
}

variable "aws_ami" {
  type        = string
  description = "The AMI to use."
}

variable "aws_subnet_id" {
  type        = string
  description = "The subnet ID to use."
}

variable "aws_security_group_id" {
  type        = string
  description = "The security group ID to use."
}

variable "aws_pem_key_name" {
  type        = string
  description = "Optional PEM key name to attach to the EC2 instances."
  default     = ""
}

variable "aws_rds_password" {
  type        = string
  description = "Password for the Amazon Aurora MySQL database."

  validation {
    condition     = length(var.aws_rds_password) >= 8 && length(var.aws_rds_password) <= 41 && length(regexall("[/'\"@ ]", var.aws_rds_password)) == 0
    error_message = "aws_rds_password must be 8-41 characters and cannot contain /, ', \", @, or spaces."
  }
}

variable "aws_ec2_instance_type" {
  type        = string
  description = "AWS EC2 instance type to use."
}

variable "common_tags" {
  type        = map(string)
  description = "Common ownership and run tags applied to taggable AWS resources."
  default     = {}
}
