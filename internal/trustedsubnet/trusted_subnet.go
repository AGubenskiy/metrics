package trustedsubnet

import (
	"fmt"
	"net"
	"strings"
)

const (
	RealIPHeader      = "X-Real-IP"
	RealIPMetadataKey = "x-real-ip"
)

type Checker struct {
	subnet *net.IPNet
}

func NewChecker(cidr string) (*Checker, error) {
	cidr = strings.TrimSpace(cidr)
	if cidr == "" {
		return &Checker{}, nil
	}

	_, subnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid trusted_subnet %q: %w", cidr, err)
	}

	return &Checker{subnet: subnet}, nil
}

func (c *Checker) Enabled() bool {
	return c != nil && c.subnet != nil
}

func (c *Checker) Allows(rawIP string) bool {
	if !c.Enabled() {
		return true
	}

	ip := net.ParseIP(strings.TrimSpace(rawIP))
	return ip != nil && c.subnet.Contains(ip)
}
