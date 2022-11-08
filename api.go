package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/pkg/errors"
	"github.com/tv42/httpunix"
)

const ServiceName = "v1"

func UnixHTTPClient(socketPath string) http.Client {
	u := &httpunix.Transport{
		DialTimeout:           100 * time.Millisecond,
		RequestTimeout:        5 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
	}
	u.RegisterLocation(ServiceName, socketPath)

	var client = http.Client{
		Transport: u,
	}
	return client
}

type StatusRequest struct {
	Status string `json:"status"`
}

func ClusterStatus(cluster string) (StatusRequest, error) {
	statusReq := StatusRequest{}
	socketPath := ApiSockPath(cluster)
	client := UnixHTTPClient(socketPath)

	url := fmt.Sprintf("http+unix://%s/status", ServiceName)
	resp, err := client.Get(url)
	if err != nil {
		return statusReq, errors.Errorf("Fail GET request: %w\n", err)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return statusReq, errors.Errorf("Failed to read response body\n")
	}

	err = json.Unmarshal(body, &statusReq)
	if err != nil {
		return statusReq, errors.Errorf("Failed to unmarshal response\n")
	}

	return statusReq, nil
}

func IsClusterRunning(cluster string) bool {
	statusReq, err := ClusterStatus(cluster)
	if err != nil {
		fmt.Printf("Failed to get cluster status for cluster %s: %w\n", cluster, err)
		return false
	}
	fmt.Printf("Cluster Status: %s\n", statusReq.Status)
	if statusReq.Status == "Running" {
		return true
	}
	return false
}

func ClusterStop(cluster string) (StatusRequest, error) {
	statusReq := StatusRequest{}
	socketPath := ApiSockPath(cluster)
	client := UnixHTTPClient(socketPath)
	url := fmt.Sprintf("http+unix://%s/exit", ServiceName)
	resp, err := client.Get(url)
	if err != nil {
		return statusReq, fmt.Errorf("Fail GET request to %s: %w\n", url, err)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return statusReq, fmt.Errorf("Failed to read response body\n")
	}
	err = json.Unmarshal(body, &statusReq)
	if err != nil {
		return statusReq, fmt.Errorf("Failed to unmarshal response\n")
	}
	fmt.Printf("Cluster Status: %s\n", statusReq.Status)
	if statusReq.Status != "Exiting" {
		return statusReq, fmt.Errorf("Failed to stop cluster %s, status: %s", cluster, statusReq.Status)
	}
	return statusReq, nil
}
