package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"runtime"
	"strings"

	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/alauda/kube-ovn/pkg/request"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/cni/pkg/version"
)

const (
	checkPointfile = "/var/lib/kubelet/device-plugins/kubelet_internal_checkpoint"
	resourceNames = ["mellanox.com/cx5_sriov_switchdev", "mellanox.com/cx4lx_sriov_switchdev"]
)

func init() {
	// this ensures that main runs only on main thread (thread group leader).
	// since namespace ops (unshare, setns) are done for a single thread, we
	// must ensure that the goroutine does not jump from OS thread to thread
	runtime.LockOSThread()
}

func main() {
	skel.PluginMain(cmdAdd, cmdDel, version.All)
}

func cmdAdd(args *skel.CmdArgs) error {
	var err error

	n, cniVersion, err := loadNetConf(args.StdinData)
	if err != nil {
		return err
	}
	podName, err := parseValueFromArgs("K8S_POD_NAME", args.Args)
	if err != nil {
		return err
	}
	podNamespace, err := parseValueFromArgs("K8S_POD_NAMESPACE", args.Args)
	if err != nil {
		return err
	}
	deviceIDs, err = getDeviceIDs(args.ContainerID)
	if err != nil {
		return err
	}

	client := request.NewCniServerClient(n.ServerSocket)

	res, err := client.Add(request.PodRequest{
		PodName:      podName,
		PodNamespace: podNamespace,
		ContainerID:  args.ContainerID,
		NetNs:        args.Netns,
		deviceIDs:    deviceIDs})
	if err != nil {
		return err
	}
	result := generateCNIResult(cniVersion, res)
	return types.PrintResult(&result, cniVersion)
}

func generateCNIResult(cniVersion string, podResponse *request.PodResponse) current.Result {
	result := current.Result{CNIVersion: cniVersion}
	_, mask, _ := net.ParseCIDR(podResponse.CIDR)
	switch podResponse.Protocol {
	case kubeovnv1.ProtocolIPv4:
		ip := current.IPConfig{
			Version: "4",
			Address: net.IPNet{IP: net.ParseIP(podResponse.IpAddress).To4(), Mask: mask.Mask},
			Gateway: net.ParseIP(podResponse.Gateway).To4(),
		}
		result.IPs = []*current.IPConfig{&ip}
		route := types.Route{}
		route.Dst = net.IPNet{IP: net.ParseIP("0.0.0.0").To4(), Mask: net.CIDRMask(0, 32)}
		route.GW = net.ParseIP(podResponse.Gateway).To4()
		result.Routes = []*types.Route{&route}
	case kubeovnv1.ProtocolIPv6:
		ip := current.IPConfig{
			Version: "6",
			Address: net.IPNet{IP: net.ParseIP(podResponse.IpAddress).To16(), Mask: mask.Mask},
			Gateway: net.ParseIP(podResponse.Gateway).To16(),
		}
		result.IPs = []*current.IPConfig{&ip}
		route := types.Route{}
		route.Dst = net.IPNet{IP: net.ParseIP("::").To16(), Mask: net.CIDRMask(0, 128)}
		route.GW = net.ParseIP(podResponse.Gateway).To16()
		result.Routes = []*types.Route{&route}
	}

	return result
}

func cmdDel(args *skel.CmdArgs) error {
	n, _, err := loadNetConf(args.StdinData)
	if err != nil {
		return err
	}

	client := request.NewCniServerClient(n.ServerSocket)
	podName, err := parseValueFromArgs("K8S_POD_NAME", args.Args)
	if err != nil {
		return err
	}
	podNamespace, err := parseValueFromArgs("K8S_POD_NAMESPACE", args.Args)
	if err != nil {
		return err
	}

	return client.Del(request.PodRequest{
		PodName:      podName,
		PodNamespace: podNamespace,
		ContainerID:  args.ContainerID,
		NetNs:        args.Netns})
}

type netConf struct {
	types.NetConf
	ServerSocket string `json:"server_socket"`
}

func loadNetConf(bytes []byte) (*netConf, string, error) {
	n := &netConf{}
	if err := json.Unmarshal(bytes, n); err != nil {
		return nil, "", fmt.Errorf("failed to load netconf: %v", err)
	}
	if n.ServerSocket == "" {
		return nil, "", fmt.Errorf("server_socket is required in cni.conf")
	}
	return n, n.CNIVersion, nil
}

func getDeviceIDs(podID) ([]string, error) {
	cpd := &types.checkpointFileData{}
	deviceIDs := []string{}
	rawBytes, err := ioutil.ReadFile(checkPointfile)
	if err != nil {
		return deviceIDs, logging.Errorf("failed to reading file %s\n%v\n", checkPointfile, err)
	}

	if err = json.Unmarshal(rawBytes, cpd); err != nil {
		return deviceIDs, logging.Errorf("failed to unmarshalling raw bytes %v", err)
	}

	for _, pod := range cpd.Data.PodDeviceEntries {
		if pod.PodUID == podID {
			for device := range resourceNames {
				if device == pod.ResourceName {
					deviceIDs = append(deviceIDs, device)
				}
			}
		}
	}
	return deviceIDs, nil
}

func parseValueFromArgs(key, argString string) (string, error) {
	if argString == "" {
		return "", errors.New("CNI_ARGS is required")
	}
	args := strings.Split(argString, ";")
	for _, arg := range args {
		if strings.HasPrefix(arg, fmt.Sprintf("%s=", key)) {
			podName := strings.TrimPrefix(arg, fmt.Sprintf("%s=", key))
			if len(podName) > 0 {
				return podName, nil
			}
		}
	}
	return "", fmt.Errorf("%s is required in CNI_ARGS", key)
}
