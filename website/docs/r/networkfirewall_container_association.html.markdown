---
subcategory: "Network Firewall"
layout: "aws"
page_title: "AWS: aws_networkfirewall_container_association"
description: |-
  Manages an AWS Network Firewall Container Association.
---

# Resource: aws_networkfirewall_container_association

Manages an AWS Network Firewall Container Association.

A container association links ECS or EKS container clusters to Network Firewall, enabling attribute-based firewall rules that use native container attributes (such as Kubernetes labels) rather than ephemeral IP addresses. This simplifies security management for dynamic container workloads.

## Example Usage

### EKS Cluster Association

```terraform
resource "aws_networkfirewall_container_association" "example" {
  container_association_name = "example-eks-association"
  type                       = "EKS"
  description                = "Association for production EKS cluster"

  container_monitoring_configurations {
    cluster_arn = aws_eks_cluster.example.arn

    attribute_filters {
      key   = "app"
      value = "backend"
    }
  }

  tags = {
    Name        = "example"
    Environment = "production"
  }
}
```

### ECS Cluster Association

```terraform
resource "aws_networkfirewall_container_association" "example" {
  container_association_name = "example-ecs-association"
  type                       = "ECS"

  container_monitoring_configurations {
    cluster_arn = aws_ecs_cluster.example.arn
  }
}
```

## Argument Reference

This resource supports the following arguments:

* `container_association_name` - (Required, Forces new resource) Descriptive name of the container association. Must contain only alphanumeric characters and hyphens, between 1 and 128 characters.
* `container_monitoring_configurations` - (Required) One or more container monitoring configurations defining which clusters to monitor. See [`container_monitoring_configurations` Block](#container_monitoring_configurations-block) below.
* `description` - (Optional) Description of the container association.
* `region` - (Optional) Region where this resource will be [managed](https://docs.aws.amazon.com/general/latest/gr/rande.html#regional-endpoints). Defaults to the Region set in the [provider configuration](https://registry.terraform.io/providers/hashicorp/aws/latest/docs#aws-configuration-reference).
* `tags` - (Optional) Map of resource tags to associate with the resource. If configured with a provider [`default_tags` configuration block](https://registry.terraform.io/providers/hashicorp/aws/latest/docs#default_tags-configuration-block) present, tags with matching keys will overwrite those defined at the provider-level.
* `type` - (Required, Forces new resource) Type of container orchestration platform. Valid values: `ECS`, `EKS`.

### `container_monitoring_configurations` Block

* `attribute_filters` - (Optional) Key-value pairs that filter which containers within the cluster are monitored. See [`attribute_filters` Block](#attribute_filters-block) below.
* `cluster_arn` - (Required) ARN of the ECS or EKS cluster to monitor.

### `attribute_filters` Block

* `key` - (Required) Key of the container attribute to filter on (for example, a Kubernetes label key).
* `value` - (Required) Value of the container attribute to filter on.

## Attribute Reference

This resource exports the following attributes in addition to the arguments above:

* `container_association_arn` - ARN of the container association.
* `last_updated_time` - Last time the container association resolved new container IP addresses.
* `resolved_cidr_count` - Number of CIDR blocks resolved from monitored containers.
* `status` - Current status of the container association. Possible values: `ACTIVE`, `CREATING`, `DELETING`.
* `tags_all` - Map of tags assigned to the resource, including those inherited from the provider [`default_tags` configuration block](https://registry.terraform.io/providers/hashicorp/aws/latest/docs#default_tags-configuration-block).
* `update_token` - Token used for optimistic locking when updating the resource.

## Timeouts

[Configuration options](https://developer.hashicorp.com/terraform/language/resources/syntax#operation-timeouts):

* `create` - (Default `30m`)
* `update` - (Default `30m`)
* `delete` - (Default `30m`)

## Import

In Terraform v1.5.0 and later, use an [`import` block](https://developer.hashicorp.com/terraform/language/import) to import Network Firewall Container Association using the `container_association_arn`. For example:

```terraform
import {
  to = aws_networkfirewall_container_association.example
  id = "arn:aws:network-firewall:us-west-2:123456789012:container-association/example"
}
```

Using `terraform import`, import Network Firewall Container Association using the `container_association_arn`. For example:

```console
% terraform import aws_networkfirewall_container_association.example arn:aws:network-firewall:us-west-2:123456789012:container-association/example
```
