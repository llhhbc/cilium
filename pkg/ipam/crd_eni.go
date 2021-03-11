package ipam

import (
	"bytes"
	"errors"
	"net"
	"sort"

	eniTypes "github.com/cilium/cilium/pkg/aws/eni/types"
	"github.com/cilium/cilium/pkg/cidr"
	"github.com/cilium/cilium/pkg/datapath/linux/linux_defaults"
	"github.com/cilium/cilium/pkg/datapath/linux/route"
	linuxrouting "github.com/cilium/cilium/pkg/datapath/linux/routing"
	ciliumv2 "github.com/cilium/cilium/pkg/k8s/apis/cilium.io/v2"
	"github.com/cilium/cilium/pkg/logging/logfields"

	"github.com/vishvananda/netlink"
)

var errNotAnIPv4Address = errors.New("not an IPv4 address")

type ciliumNodeENIRulesAndRoutesOptions struct {
	EgressMultiHomeIPRuleCompat bool
	EnableIPv4Masquerade        bool
}

// ciliumNodeENIRulesAndRoutes returns the rules and routes required to configure
func ciliumNodeENIRulesAndRoutes(node *ciliumv2.CiliumNode, macToNetlinkInterfaceIndex map[string]int, options ciliumNodeENIRulesAndRoutesOptions) (rules []*route.Rule, routes []*netlink.Route) {
	subnets := make([]*cidr.CIDR, 0, len(node.Status.ENI.Subnets))
	for _, subnetString := range node.Status.ENI.Subnets {
		if subnet, err := cidr.ParseCIDR(subnetString); err == nil {
			subnets = append(subnets, subnet)
		}
	}

	// Extract the used IPs by ENI from node.Status.IPAM.Used.
	ipsByResource := make(map[string][]net.IP)
	firstInterfaceIndex := *node.Spec.ENI.FirstInterfaceIndex
	for address, allocationIP := range node.Status.IPAM.Used {
		resource := allocationIP.Resource
		eni, ok := node.Status.ENI.ENIs[resource]
		if !ok {
			log.WithField(logfields.Resource, resource).Warning("ignoring unknown resource")
			continue
		}
		if eni.Number < firstInterfaceIndex {
			continue
		}
		ip := net.ParseIP(address)
		if ip == nil {
			log.WithField(logfields.IPAddr, address).Warning("ignoring non-IPv4 address")
			continue
		}
		ipsByResource[resource] = append(ipsByResource[resource], ip)
	}

	// Sort ENIs and IPs so the order of rules and routes is deterministic.
	resourcesByNumber := make([]string, 0, len(ipsByResource))
	for eni, ips := range ipsByResource {
		resourcesByNumber = append(resourcesByNumber, eni)
		sort.Slice(ips, func(i, j int) bool {
			return bytes.Compare(ips[i], ips[j]) < 0
		})
	}
	sort.Slice(resourcesByNumber, func(i, j int) bool {
		return node.Status.ENI.ENIs[resourcesByNumber[i]].Number < node.Status.ENI.ENIs[resourcesByNumber[j]].Number
	})

	var egressPriority int
	if options.EgressMultiHomeIPRuleCompat {
		egressPriority = linux_defaults.RulePriorityEgress
	} else {
		egressPriority = linux_defaults.RulePriorityEgressv2
	}

	for _, resource := range resourcesByNumber {
		eni := node.Status.ENI.ENIs[resource]
		netlinkInterfaceIndex, ok := macToNetlinkInterfaceIndex[eni.MAC]
		if !ok {
			log.WithField(logfields.MACAddr, eni.MAC).Warning("failed to retrieve netlink interface index")
			continue
		}

		gateway, err := subnetGatewayAddress(eni.Subnet)
		if err != nil {
			log.WithError(err).WithField(logfields.CIDR, eni.Subnet).Warning("failed to determine gateway address")
			continue
		}

		var tableID int
		if options.EgressMultiHomeIPRuleCompat {
			tableID = netlinkInterfaceIndex
		} else {
			tableID = linuxrouting.ComputeTableIDFromIfaceNumber(eni.Number)
		}

		// Generate rules for each IPs.
		for _, ip := range ipsByResource[resource] {
			ipWithMask := net.IPNet{
				IP:   ip,
				Mask: net.CIDRMask(32, 32),
			}

			// On ingress, route all traffic to the endpoint IP via the main
			// routing table. Egress rules are created in a per-ENI routing
			// table.
			ingressRule := &route.Rule{
				Priority: linux_defaults.RulePriorityIngress,
				To:       &ipWithMask,
				Table:    route.MainTable,
			}
			rules = append(rules, ingressRule)

			if options.EnableIPv4Masquerade {
				// Lookup a VPC specific table for all traffic from an endpoint
				// to the CIDR configured for the VPC on which the endpoint has
				// the IP on.
				egressRules := make([]*route.Rule, 0, len(subnets))
				for _, subnet := range subnets {
					egressRule := &route.Rule{
						Priority: egressPriority,
						From:     &ipWithMask,
						To:       subnet.IPNet,
						Table:    tableID,
					}
					egressRules = append(egressRules, egressRule)
				}
				rules = append(rules, egressRules...)
			} else {
				// Lookup a VPC specific table for all traffic from an endpoint.
				egressRule := &route.Rule{
					Priority: egressPriority,
					From:     &ipWithMask,
					Table:    tableID,
				}
				rules = append(rules, egressRule)
			}
		}

		// Generate routes.

		// Nexthop route to the VPC or subnet gateway. Note: This is a /32 route
		// to avoid any L2. The endpoint does no L2 either.
		nexthopRoute := &netlink.Route{
			LinkIndex: netlinkInterfaceIndex,
			Dst: &net.IPNet{
				IP:   gateway,
				Mask: net.CIDRMask(32, 32),
			},
			Scope: netlink.SCOPE_LINK,
			Table: tableID,
		}
		routes = append(routes, nexthopRoute)

		// Default route to the VPC or subnet gateway.
		defaultRoute := &netlink.Route{
			Dst: &net.IPNet{
				IP:   net.IPv4zero,
				Mask: net.CIDRMask(0, 32),
			},
			Table: tableID,
			Gw:    gateway,
		}
		routes = append(routes, defaultRoute)
	}

	return
}

// subnetGatewayAddress returns the address of the subnet's gateway.
func subnetGatewayAddress(subnet eniTypes.AwsSubnet) (net.IP, error) {
	subnetIP, _, err := net.ParseCIDR(subnet.CIDR)
	if err != nil {
		return nil, err
	}

	if subnetIP.To4() == nil {
		return nil, errNotAnIPv4Address
	}

	// The gateway for a subnet and VPC is always x.x.x.1, see
	// https://docs.aws.amazon.com/vpc/latest/userguide/VPC_Route_Tables.html.
	subnetIP[len(subnetIP)-1]++

	return subnetIP, nil
}
