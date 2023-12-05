// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of Cilium

package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/cilium/cilium/pkg/command"
	"github.com/cilium/cilium/pkg/common"
	"github.com/cilium/cilium/pkg/maps/tunnel"
)

const (
	tunnelTitle      = "TUNNEL"
	destinationTitle = "VALUE"
)

var bpfTunnelListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List tunnel endpoint entries",
	Run: func(cmd *cobra.Command, args []string) {
		common.RequireRootPrivilege("cilium bpf tunnel list")

		tunnelList := make(map[string][]string)
		if err := tunnel.TunnelMap.Dump(tunnelList); err != nil {
			os.Exit(1)
		}

		if command.OutputOption() {
			if err := command.PrintOutput(tunnelList); err != nil {
				os.Exit(1)
			}
			return
		}

		TablePrinter(tunnelTitle, destinationTitle, tunnelList)
	},
}

func init() {
	bpfTunnelCmd.AddCommand(bpfTunnelListCmd)
	command.AddOutputOption(bpfTunnelListCmd)
	bpfTunnelCmd.AddCommand(bpfTunnelSetCmd)
	command.AddOutputOption(bpfTunnelSetCmd)

	bpfTunnelSetCmd.Flags().IP("endpointCidr", nil, "tunnel endpoint cidr. ")
	bpfTunnelSetCmd.Flags().IP("destIp", nil, "tunnel dest ip. ")
}

var bpfTunnelSetCmd = &cobra.Command{
	Use:     "set",
	Aliases: []string{"set"},
	Short:   "Set tunnel endpoint ",
	Run: func(cmd *cobra.Command, args []string) {
		common.RequireRootPrivilege("cilium bpf tunnel set")

		endpointCidr, err := cmd.Flags().GetIP("endpointCidr")
		if err != nil {
			log.Errorf("endpoint cidr must set. ")
			os.Exit(1)
		}
		destIp, err := cmd.Flags().GetIP("destIp")
		if err != nil {
			log.Errorf("destIP must set. ")
			os.Exit(1)
		}

		err = tunnel.TunnelMap.SetTunnelEndpoint(0, endpointCidr, destIp)
		if err != nil {
			log.Errorf("set tunnel endpoint failed %v. ", err)
		}
		log.Printf("set tunnel ok. ")
	},
}
