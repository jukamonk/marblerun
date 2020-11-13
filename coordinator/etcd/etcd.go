package etcd

import (
	"fmt"
	"net"
	"net/url"

	"go.etcd.io/etcd/embed"
)

// ClusterInfo stores information related to an etcd cluster
type ClusterInfo struct {
	Namespace   string
	ClusterName string
	ClusterSize int
	Peers       []string
}

// GetConfig returns an etcd config for a coordinator node
func GetConfig(nodeName string, ci *ClusterInfo, debug bool) *embed.Config {
	cfg := embed.NewConfig()
	cfg.Dir = "default.etcd"
	cfg.Debug = debug
	cfg.APUrls = parsePeers([]string{ci.GetFQDN(nodeName)})
	cfg.LPUrls = parsePeers([]string{"0.0.0.0"})
	cfg.ACUrls = parseClients([]string{ci.GetFQDN(nodeName)})
	cfg.LCUrls = parseClients([]string{"0.0.0.0"})
	cfg.InitialCluster = parseInitialCluster(ci)
	return cfg
}

// NewEtcdClusterInfo returns a new cluster info
func NewEtcdClusterInfo(namespace, name string, size int) *ClusterInfo {
	eci := &ClusterInfo{
		Namespace:   namespace,
		ClusterName: name,
		ClusterSize: size,
	}
	peers := make([]string, size)
	for i := 0; i < size; i++ {
		peers[i] = eci.GetFQDN(fmt.Sprintf("%s-%d", name, i))
	}
	eci.Peers = peers
	return eci
}

// GetFQDN converts an etcd node name to its FQDN
func (c *ClusterInfo) GetFQDN(nodeName string) string {
	return fmt.Sprintf("%s.%s.%s.svc.cluster.local", nodeName, c.ClusterName, c.Namespace)
}

func parsePeers(eps []string) []url.URL {
	neps := make([]url.URL, len(eps))
	for i, ep := range eps {
		u := url.URL{Scheme: "https", Host: net.JoinHostPort(ep, "2380")}
		neps[i] = u
	}
	return neps
}

func parseClients(eps []string) []url.URL {
	neps := make([]url.URL, len(eps))
	for i, ep := range eps {
		u := url.URL{Scheme: "https", Host: net.JoinHostPort(ep, "2379")}
		neps[i] = u
	}
	return neps
}

func parseInitialCluster(ci *ClusterInfo) string {
	neps := ""
	for i, peer := range ci.Peers {
		u := url.URL{Scheme: "https", Host: net.JoinHostPort(peer, "2380")}
		neps += fmt.Sprintf("%s-%d=%s", ci.ClusterName, i, u.String())
		if i+1 < len(ci.Peers) {
			neps += ","
		}
	}
	return neps
}
