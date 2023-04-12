// Copyright 2018 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

//go:build enablek8sconnector
// +build enablek8sconnector

package config

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"

	"github.com/aws/amazon-vpc-cni-plugins/network/vpc"
)

func init() {
	retrievePodConfigHandler = retrievePodConfig
}

// retrievePodConfig retrieves a pod's configuration from an external source.
func retrievePodConfig(netConfig *NetConfig) error {

	kc := netConfig.Kubernetes

	// retrieve Pod IP Address CIDR string using podName, podNamespace
	ipAddress, err := retrievePodIPWithK8sConnector(kc.PodName, kc.Namespace)
	if err != nil {
		return fmt.Errorf("failed to get pod IP %s: %w", kc.PodName, err)
	}

	ipAddr, err := vpc.GetIPAddressFromString(ipAddress)
	if err != nil {
		return fmt.Errorf("invalid IPAddress %s from pod label", ipAddress)
	}
	netConfig.IPAddresses = append(netConfig.IPAddresses, *ipAddr)

	return nil
}

// retrievePodIPWithK8sConnector retrieves Pod IP using k8s connector binary.
func retrievePodIPWithK8sConnector(podName, podNamespace string) (string, error) {
	// Get Path to aws-vpc-cni-k8s-connector binary
	connectorBinaryPath := os.Getenv("AWS_VPC_CNI_K8S_CONNECTOR_BINARY_PATH")

	// Prepare command to execute binary with required args
	cmd := exec.Command(connectorBinaryPath, "-pod-name", podName, "-pod-namespace", podNamespace)

	var errBytes, outputBytes bytes.Buffer
	cmd.Stderr = &errBytes
	cmd.Stdout = &outputBytes

	// Run command
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("retrievePodIPWithK8sConnector: error running connector %w", err)
	}

	// Return error if exit code is other than 0
	if cmd.ProcessState.ExitCode() != 0 {
		return "", fmt.Errorf("retrievePodIPWithK8sConnector: failure in connector %s", errBytes.String())
	}

	return outputBytes.String(), nil
}
