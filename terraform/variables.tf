variable "instance_name" {
  description = "Unique name used to distinguish one deployment from another."
}

variable "owner" {
  description = "Owner name/email used to tag all AWS resources created by this module."
}

variable "function_file" {
  description = "Name of the zip file with the lambda function."
}

variable "db_region_config" {
  description = "DynamoDB region configuration."
  type = object({
    primary  = string
    replicas = set(string)
  })
}

variable "kafka_endpoint" {
  description = "Host and port of Kafka server."
}

variable "guide_cells_kafka_topic" {
  description = "Name of Kafka topic with GuideCell objects."
}

variable "vpc_subnets" {
  description = "VPC subnets to contact Kafka server."
  type        = list(string)
}

variable "vpc_security_group" {
  description = "VPC security group to contact Kafka server."
}

variable "shared_sns_topic_for_alarms" {
  description = "Shared SNS topic for alarms. Note: used by most/all serverless services."
}

variable "create_dedicated_sns_topic_for_alarms" {
  description = "Controls creation of a dedicated SNS topic for alarms with email subscription to the value of 'owner' variable."
  default     = true
}
