package wireguard

import (
	"fmt"
	"net"

	v2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/cilium/cilium/pkg/lock"
	"github.com/cilium/cilium/pkg/node/addressing"

	"github.com/cilium/ipam/service/ipallocator"
	"k8s.io/client-go/util/retry"
)

type CiliumNodeUpdater interface {
	Update(origNode, node *v2.CiliumNode) (*v2.CiliumNode, error)
	Get(node string) (*v2.CiliumNode, error)
}

type Manager struct {
	lock.RWMutex
	ipAlloc                   *ipallocator.Range
	restoring                 bool
	allocForNodesAfterRestore map[string]struct{}
	ciliumNodeUpdater         CiliumNodeUpdater
}

func NewManager(subnetV4 *net.IPNet, ciliumNodeUpdater CiliumNodeUpdater) (*Manager, error) {
	alloc, err := ipallocator.NewCIDRRange(subnetV4)
	if err != nil {
		return nil, err
	}

	m := &Manager{
		ipAlloc:                   alloc,
		restoring:                 true,
		allocForNodesAfterRestore: make(map[string]struct{}),
		ciliumNodeUpdater:         ciliumNodeUpdater,
	}

	return m, nil
}

func (m *Manager) AddNode(n *v2.CiliumNode) error {
	m.Lock()
	defer m.Unlock()

	return m.allocateIP(n, false)
}

func (m *Manager) UpdateNode(n *v2.CiliumNode) error {
	m.Lock()
	defer m.Unlock()

	return m.allocateIP(n, true)
}

func (m *Manager) DeleteNode(n *v2.CiliumNode) {
	m.Lock()
	defer m.Unlock()

	if m.restoring {
		panic("INVALID STATE") // TODO log err
	}

	found := false
	var ip net.IP
	for _, addr := range n.Spec.Addresses {
		if addr.Type == addressing.NodeWireguardIP {
			ip = net.ParseIP(addr.IP)
			if ip.To4() != nil {
				found = true
				break
			}
		}
	}

	if found {
		if m.restoring {
			delete(m.allocForNodesAfterRestore, n.ObjectMeta.Name)
		}
		m.ipAlloc.Release(ip)
	}
}

func (m *Manager) Resync() error {
	m.Lock()
	defer m.Unlock()

	m.restoring = false
	for nodeName := range m.allocForNodesAfterRestore {
		ip, err := m.ipAlloc.AllocateNext()
		if err != nil {
			return fmt.Errorf("failed to allocate IP addr for node %s: %w", nodeName)
		}
		if err := m.setCiliumNodeIP(nodeName, ip); err != nil {
			return err
		}
	}

	return nil
}

// allocateIP must be called with *Manager mutex being held.
func (m *Manager) allocateIP(n *v2.CiliumNode, isUpdate bool) error {
	found := false
	var ip net.IP
	for _, addr := range n.Spec.Addresses {
		if addr.Type == addressing.NodeWireguardIP {
			ip = net.ParseIP(addr.IP)
			if ip.To4() != nil {
				found = true
				break
			}
		}
	}

	fmt.Println("!!! found", found, ip)

	if !found {
		if m.restoring {
			m.allocForNodesAfterRestore[n.ObjectMeta.Name] = struct{}{}
		} else {
			ip, err := m.ipAlloc.AllocateNext()
			if err != nil {
				return fmt.Errorf("failed to allocate IP addr for node %s: %w", n.ObjectMeta.Name)
			}

			if err := m.setCiliumNodeIP(n.ObjectMeta.Name, ip); err != nil {
				return err
			}
		}
	} else if !isUpdate {
		if err := m.ipAlloc.Allocate(ip); err != nil {
			return fmt.Errorf("failed to re-allocate IP addr %s for node %s: %w", ip, n.ObjectMeta.Name, err)
		}
	}

	return nil
}

func (m *Manager) setCiliumNodeIP(nodeName string, ip net.IP) error {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		node, err := m.ciliumNodeUpdater.Get(nodeName)
		if err != nil {
			return err
		}

		node.Spec.Addresses = append(node.Spec.Addresses, v2.NodeAddress{Type: addressing.NodeWireguardIP, IP: ip.String()})
		_, err = m.ciliumNodeUpdater.Update(nil, node)
		return err
	})
	return err

}
