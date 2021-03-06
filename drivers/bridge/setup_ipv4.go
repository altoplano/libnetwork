package bridge

import (
	"net"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/libnetwork/netutils"
	"github.com/vishvananda/netlink"
)

var bridgeNetworks []*net.IPNet

func init() {
	// Here we don't follow the convention of using the 1st IP of the range for the gateway.
	// This is to use the same gateway IPs as the /24 ranges, which predate the /16 ranges.
	// In theory this shouldn't matter - in practice there's bound to be a few scripts relying
	// on the internal addressing or other stupid things like that.
	// They shouldn't, but hey, let's not break them unless we really have to.
	for _, addr := range []string{
		"172.17.42.1/16", // Don't use 172.16.0.0/16, it conflicts with EC2 DNS 172.16.0.23
		"10.0.42.1/16",   // Don't even try using the entire /8, that's too intrusive
		"10.1.42.1/16",
		"10.42.42.1/16",
		"172.16.42.1/24",
		"172.16.43.1/24",
		"172.16.44.1/24",
		"10.0.42.1/24",
		"10.0.43.1/24",
		"192.168.42.1/24",
		"192.168.43.1/24",
		"192.168.44.1/24",
	} {
		ip, net, err := net.ParseCIDR(addr)
		if err != nil {
			log.Errorf("Failed to parse address %s", addr)
			continue
		}
		net.IP = ip
		bridgeNetworks = append(bridgeNetworks, net)
	}
}

func setupBridgeIPv4(config *Configuration, i *bridgeInterface) error {
	bridgeIPv4, err := electBridgeIPv4(config)
	if err != nil {
		return err
	}

	log.Debugf("Creating bridge interface %q with network %s", config.BridgeName, bridgeIPv4)
	if err := netlink.AddrAdd(i.Link, &netlink.Addr{IPNet: bridgeIPv4}); err != nil {
		return &IPv4AddrAddError{ip: bridgeIPv4, err: err}
	}

	// Store bridge network and default gateway
	i.bridgeIPv4 = bridgeIPv4
	i.gatewayIPv4 = i.bridgeIPv4.IP

	return nil
}

func electBridgeIPv4(config *Configuration) (*net.IPNet, error) {
	// Use the requested IPv4 CIDR when available.
	if config.AddressIPv4 != nil {
		return config.AddressIPv4, nil
	}

	// We don't check for an error here, because we don't really care if we
	// can't read /etc/resolv.conf. So instead we skip the append if resolvConf
	// is nil. It either doesn't exist, or we can't read it for some reason.
	nameservers := []string{}
	if resolvConf, _ := readResolvConf(); resolvConf != nil {
		nameservers = append(nameservers, getNameserversAsCIDR(resolvConf)...)
	}

	// Try to automatically elect appropriate bridge IPv4 settings.
	for _, n := range bridgeNetworks {
		if err := netutils.CheckNameserverOverlaps(nameservers, n); err == nil {
			if err := netutils.CheckRouteOverlaps(n); err == nil {
				return n, nil
			}
		}
	}

	return nil, IPv4AddrRangeError(config.BridgeName)
}

func setupGatewayIPv4(config *Configuration, i *bridgeInterface) error {
	if !i.bridgeIPv4.Contains(config.DefaultGatewayIPv4) {
		return ErrInvalidGateway
	}
	if _, err := ipAllocator.RequestIP(i.bridgeIPv4, config.DefaultGatewayIPv4); err != nil {
		return err
	}

	// Store requested default gateway
	i.gatewayIPv4 = config.DefaultGatewayIPv4

	return nil
}
