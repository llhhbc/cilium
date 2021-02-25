package wireguard

import (
	"fmt"
	"net"

	v2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/cilium/cilium/pkg/node/addressing"

	"github.com/cilium/ipam/service/ipallocator"
)

type Manager struct {
	ipAlloc                   *ipallocator.Range
	restoring                 bool
	allocForNodesAfterRestore []string
}

func NewManager(subnetV4 *net.IPNet) (*Manager, error) {
	alloc, err := ipallocator.NewCIDRRange(subnetV4)
	if err != nil {
		return nil, err
	}

	m := &Manager{
		ipAlloc:                   alloc,
		restoring:                 true,
		allocForNodesAfterRestore: make([]string, 0),
	}

	return m, nil
}

func (m *Manager) AddNode(n *v2.CiliumNode) error {
	fmt.Println("LOL AddNode", n)

	found := false
	var ip net.IP
	for _, addr := range n.Spec.Addresses {
		if addr.Type == addressing.NodeWireguardIP {
			ip = net.ParseIP(addr.IP)
			if ip.To4() != nil {
				found = true

			}
		}
	}

	if !found {
		if m.restoring {
			m.allocForNodesAfterRestore = append(m.allocForNodesAfterRestore, n.ObjectMeta.Name)
		} else {
			ip, err := m.ipAlloc.AllocateNext()
			if err != nil {
				return fmt.Errorf("failed to allocate IP addr for node %s: %w", n.ObjectMeta.Name)
			}
			fmt.Println("TODO", ip)
			// TODO set ip in the ciliumNode obj
		}
	} else {
		err := m.ipAlloc.Allocate(ip)
		if err != nil {
			return fmt.Errorf("failed to re-allocate IP addr %s for node %s: %w", ip, n.ObjectMeta.Name)
		}
	}

	return nil
}

func (m *Manager) UpdateNode(n *v2.CiliumNode) {
	fmt.Println("LOL UpdateNode", n)

}

func (m *Manager) DeleteNode(n *v2.CiliumNode) {
	fmt.Println("LOL DeleteNode", n)
}

func (m *Manager) Resync() error {
	m.restoring = false
	for _, nodeName := range m.allocForNodesAfterRestore {
		ip, err := m.ipAlloc.AllocateNext()
		if err != nil {
			return fmt.Errorf("failed to allocate IP addr for node %s: %w", nodeName)
		}
		// TODO update ciliumnode obj
		fmt.Println("TODO", ip)
	}

	return nil
}
