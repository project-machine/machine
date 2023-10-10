package client

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"path/filepath"
	"time"
	"net"
	"net/http"

	"github.com/go-resty/resty/v2"

	"github.com/project-machine/machine/pkg/api"
)

var rootclient *resty.Client

func init() {
	rand.Seed(time.Now().UTC().UnixNano())

	// configure the http client to point to the unix socket
	apiSocket := api.APISocketPath()
	if len(apiSocket) == 0 {
		panic("Failed to get API socket path")
	}

	unixDial := func(_ context.Context, network, addr string) (net.Conn, error) {
		raddr, err := net.ResolveUnixAddr("unix", apiSocket)
		if err != nil {
			return nil, err
		}

		return net.DialUnix("unix", nil, raddr)
	}

	transport := http.Transport{
		DialContext:           unixDial,
		DisableKeepAlives:     true,
		ExpectContinueTimeout: time.Second * 30,
		ResponseHeaderTimeout: time.Second * 3600,
		TLSHandshakeTimeout:   time.Second * 5,
	}

	rootclient = resty.New()
	rootclient.SetTransport(&transport).SetScheme("http").SetBaseURL(apiSocket)
}

func GetMachines() ([]api.Machine, error) {
	machines := []api.Machine{}
	listURL := api.GetAPIURL("machines")
	if len(listURL) == 0 {
		return machines, fmt.Errorf("Failed to get API URL for 'machines' endpoint")
	}
	resp, _ := rootclient.R().EnableTrace().Get(listURL)
	err := json.Unmarshal(resp.Body(), &machines)
	if err != nil {
		return machines, fmt.Errorf("Failed to unmarshal GET on /machines")
	}
	return machines, nil
}

func GetMachine(machineName string) (api.Machine, int, error) {
	machine := api.Machine{}
	getURL := api.GetAPIURL(filepath.Join("machines", machineName))
	if len(getURL) == 0 {
		return machine, http.StatusBadRequest, fmt.Errorf("Failed to get API URL for 'machines/%s' endpoint", machineName)
	}
	resp, _ := rootclient.R().EnableTrace().Get(getURL)
	err := json.Unmarshal(resp.Body(), &machine)
	if err != nil {
		return machine, resp.StatusCode(), fmt.Errorf("%d: Failed to unmarshal GET on /machines/%s", resp.StatusCode(), machineName)
	}
	return machine, resp.StatusCode(), nil
}

func PutMachine(newMachine api.Machine) error {
	endpoint := fmt.Sprintf("machines/%s", newMachine.Name)
	putURL := api.GetAPIURL(endpoint)
	if len(putURL) == 0 {
		return fmt.Errorf("Failed to get API PUT URL for 'machines' endpoint")
	}
	resp, err := rootclient.R().EnableTrace().SetBody(newMachine).Put(putURL)
	if err != nil {
		return fmt.Errorf("Failed PUT to machine '%s' endpoint: %s", newMachine.Name, err)
	}
	fmt.Printf("%s %s\n", resp, resp.Status())
	return nil
}
