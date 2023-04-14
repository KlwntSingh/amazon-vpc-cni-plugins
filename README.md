## Amazon VPC CNI Plugins

VPC CNI plugins for Amazon ECS and Amazon EKS.

# Pre-requisites for running CNI plugin for EKS Windows
1. Env variable  `AWS_VPC_CNI_K8S_CONNECTOR_BINARY_PATH` is required to be set. This will be already set in EKS Windows Optimized AMIs.
Set env variable `AWS_VPC_CNI_K8S_CONNECTOR_BINARY_PATH` to `C:\Program Files\Amazon\EKS\cni\aws-vpc-cni-k8s-connector.exe`.

## Security disclosures

If you think you’ve found a potential security issue, please do not post it in the Issues. Instead, please follow the instructions [here](https://aws.amazon.com/security/vulnerability-reporting/) or [email AWS security directly](mailto:aws-security@amazon.com).

## License

This library is licensed under the Apache 2.0 License.
