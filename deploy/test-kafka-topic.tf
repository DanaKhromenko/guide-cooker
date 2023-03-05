locals {
  name = "guide-cooker-${var.instance_name}"
}

provider "test_provider" {
  datacenter = "test.provider.com"
}

# Note: topic config must be kept in sync with actual Kafka topics created by UDF.
resource "test_dynconfig_topic" "guide_cells_kafka_topic" {
  name                    = "${local.name}.test.guide-cells"
  archive_policy          = "none"
  backup_policy           = "none"
  cleanup_policy_duration = "oneDay"
  cleanup_policy_type     = "dynconfigTopicTombstoneRetentionPolicy"
  configuration_policy    = "sourceOfTruth"
  container               = local.name
  environment             = local.name
  partition_policy        = "normal"
  privacy_policy          = "none"
  service                 = local.name
  visibility_policy       = "public"
  allow_overwrite         = true
}

output "guide_cells_kafka_topic_name" {
  value = test_dynconfig_topic.guide_cells_kafka_topic.name
}
