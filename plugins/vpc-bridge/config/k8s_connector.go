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

package config

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/aws/amazon-vpc-cni-plugins/network/vpc"

	winio "github.com/Microsoft/go-winio"
	"github.com/Microsoft/go-winio/pkg/guid"
	log "github.com/cihub/seelog"
)

// Represents output of k8s connector received from named pipe
type k8sExecutionResult struct {
	stdout string // Represents message with required data.
	stderr error  // Represents error while getting output.
}

const (
	// Represents timeout for reading over results channel.
	// Represents timeout for reading from connection object.
	k8sExecutionTimeout = 3 * time.Minute
)

func init() {
	retrievePodConfigHandler = retrievePodConfig
}

// retrievePodConfig retrieves a pod's configuration from an external source.
func retrievePodConfig(netConfig *NetConfig) error {

	kc := netConfig.Kubernetes

	// retrieve Pod IP Address CIDR string using podName, podNamespace.
	ipAddress, err := retrievePodIPWithK8sConnector(kc.Namespace, kc.PodName)
	if err != nil {
		return fmt.Errorf("failed to get pod IP %s: %w", kc.PodName, err)
	}

	// Parse IP address returned by k8s binary.
	ipAddr, err := vpc.GetIPAddressFromString(ipAddress)
	if err != nil {
		return fmt.Errorf("invalid IPAddress %s from pod label", ipAddress)
	}
	netConfig.IPAddresses = append(netConfig.IPAddresses, *ipAddr)

	return nil
}

// retrievePodIPWithK8sConnector retrieves Pod IP with k8s connector binary using named pipe.
func retrievePodIPWithK8sConnector(podNamespace, podName string) (string, error) {

	// Create named pipe, used to get output from k8s connector binary.
	// pipeListener will accept connections in separate go routine. Output of binary is received via channel.
	pipeListener, executionResultChan, err := createNamePipeForOutputFromK8sConnector()
	if err != nil {
		return "", fmt.Errorf("retrievePodIPWithK8sConnector: error creating named pipe %w", err)
	}
	defer pipeListener.Close()       // Defer closing named pipe listener.
	defer close(executionResultChan) // Defer closing result channel.

	namedPipePath := getPipeName(pipeListener)
	log.Debugf("named piped: %s created", namedPipePath)

	// Execute k8s connector binary with pod namespace, pod name and named pipe path.
	err = executeK8sConnector(podNamespace, podName, namedPipePath)
	if err != nil {
		return "", fmt.Errorf("retrievePodIPWithK8sConnector: error executing k8s connector %w", err)
	}

	// Read output of k8s binary from named pipe.
	var result k8sExecutionResult
	select {
	case result = <-executionResultChan:
	case <-time.After(k8sExecutionTimeout):
		return "", fmt.Errorf("retrievePodIPWithK8sConnector: timeout getting output of k8s connector")
	}

	if result.stderr != nil {
		return "", fmt.Errorf("retrievePodIPWithK8sConnector: error in k8s connector %w", result.stderr)
	}
	podIP := result.stdout
	log.Debugf("got pod IP: %s for pod %s in namespace %s", podIP, podName, podNamespace)

	return podIP, nil
}

// executeK8sConnector executes aws-vpc-cni-k8s-connector binary and get pod IP over named pipe.
// Execution logs from binary are stderr.
func executeK8sConnector(podNamespace, podName, pipe string) error {
	// Get Path to aws-vpc-cni-k8s-connector binary from env variable.
	connectorBinaryPath := os.Getenv("AWS_VPC_CNI_K8S_CONNECTOR_BINARY_PATH")
	// Use VPC CNI log level for k8s connector binary.
	binaryLogLevel := os.Getenv("VPC_CNI_LOG_LEVEL")

	// Prepare command to execute binary with required args.
	cmd := exec.Command(connectorBinaryPath,
		"-pod-name", podName, "-pod-namespace", podNamespace,
		"-pipe", pipe, "-log-level", binaryLogLevel)
	log.Debugf("Executing cmd: %s to get pod IP", cmd.String())

	var errBytes, outputBytes bytes.Buffer
	cmd.Stderr = &errBytes
	cmd.Stdout = &outputBytes

	// Run command
	err := cmd.Run()
	if err != nil || cmd.ProcessState.ExitCode() != 0 {
		return fmt.Errorf("executeK8sConnector: error running connector binary %w with error: %s", err, errBytes.String())
	}

	log.Infof("logs from k8s connector binary...\n%s ...end of k8s connector binary logs", errBytes.String())

	return nil
}

// createNamePipeForOutputFromK8sConnector creates named pipe.
// Starts listening and reading named pipe in go routine.
// Returns listener for named pipe and channel for receiving output from channel.
func createNamePipeForOutputFromK8sConnector() (net.Listener, chan k8sExecutionResult, error) {
	// Generate GUID random string.
	g, err := guid.NewV4()
	if err != nil {
		return nil, nil, fmt.Errorf("createNamePipeForOutputFromK8sConnector: error creating unique name for named pipe %w", err)
	}
	// Pipe name with k8s connector prefix.
	pipeName := fmt.Sprintf(`\\.\pipe\aws-vpc-k8s-connector-%s`, g.String())

	// Create pipe and get listener.
	// listener is closed in main routine, to better handle timeout case.
	pipeListener, err := winio.ListenPipe(pipeName, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("createNamePipeForOutputFromK8sConnector: error creating named pipe:%s %w", pipeName, err)
	}

	// Creating result channel to get k8s connector output.
	result := make(chan k8sExecutionResult)

	// Accept connections and read pipe in go routine as these operations are blocking.
	go readFromNamePipe(pipeListener, result)

	return pipeListener, result, nil
}

// getPipeName get name/path for named pipe using pipe listener.
func getPipeName(listener net.Listener) string {
	return listener.Addr().String()
}

// readFromNamePipe start accepting connections and read message from named pipe.
// This runs in go routine as Accept and Read from conn as blocking operations.
// Result is sent over result channel.
// Timeout for read operation on conn.
func readFromNamePipe(pipeListener net.Listener, result chan k8sExecutionResult) {
	// Get named pipe path
	pipNamePath := getPipeName(pipeListener)

	// Accept connection on named pipe. This operation is blocking until client dials into pipe.
	conn, err := pipeListener.Accept()
	if err != nil {
		// Send error over channel
		result <- k8sExecutionResult{stderr: fmt.Errorf("readFromNamePipe: error accepting connection on pipe listener %v %w", pipNamePath, err)}
		return
	}
	defer conn.Close() // Defer closing connection.

	// Setup read timeout on conn object.
	err = conn.SetReadDeadline(time.Now().Add(3 * time.Minute))
	if err != nil {
		// Send error over channel
		result <- k8sExecutionResult{stderr: fmt.Errorf("readFromNamePipe: error setting timeout for pipe connection %v %w", pipNamePath, err)}
		return
	}

	// Get message from named pipe. This operation is blocking until client has written a message.
	// Returns error if message not available within above given timeout.
	var output bytes.Buffer
	_, err = io.Copy(&output, conn) // Copy message from pipe connection to buffer.
	if err != nil {
		// Send error over channel
		result <- k8sExecutionResult{stderr: fmt.Errorf("readFromNamePipe: error reading from named piped %v %w", pipNamePath, err)}
		return
	}

	// Send output from named pipe on channel
	result <- k8sExecutionResult{stdout: output.String()}
	return
}
