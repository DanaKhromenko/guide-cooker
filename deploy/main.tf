variable "instance_name" {}
variable "owner" {}

terraform {
  backend "s3" {
    bucket         = "test-terraform-storage"
    dynamodb_table = "test-terraform-locking"
    encrypt        = true
    region         = "us-west-2"
  }
}

provider "aws" {
  region = "us-east-1"
}

data "terraform_remote_state" "shared" {
  backend = "s3"
  config = {
    bucket         = "test-terraform-storage"
    dynamodb_table = "test-terraform-locking"
    encrypt        = true
    region         = "us-west-2"
    key            = "shared/terraform.tfstate"
  }
}

module "guide-cooker" {
  source                      = "../terraform"
  instance_name               = var.instance_name
  owner                       = var.owner
  function_file               = "../guide-cooker.zip"
  kafka_endpoint              = "kafka.test.com:1234"
  guide_cells_kafka_topic     = test_dynconfig_topic.guide_cells_kafka_topic.name
  vpc_subnets                 = data.terraform_remote_state.shared.outputs.internal-vpc-subnets-us-east-1
  vpc_security_group          = data.terraform_remote_state.shared.outputs.internal-vpc-security-group-id-us-east-1
  shared_sns_topic_for_alarms = data.terraform_remote_state.shared.outputs.cloudwatch-alarms-ignore-topic-us-east-1-arn
  db_region_config = {
    primary = "us-east-1"
    # Note: this is necessary to test replication during development and pipeline builds.
    replicas = ["us-west-2"]
  }
  create_dedicated_sns_topic_for_alarms = false
}

output "guide-cooker" {
  value = module.guide-cooker
}
