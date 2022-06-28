package util

import (
	"errors"
	"net"
)

// ErrBadIPv4 is a generic error that an IP address could not be parsed
var ErrBadIPv4 = errors.New("bad IPv4 address")

// FromNetIPv4 converts a IPv4 net.IP to uint32
func FromNetIPv4(ip net.IP) uint32 {
	ip = ip.To4()
	if ip == nil {
		panic(ErrBadIPv4)
	}
	return uint32(ip[3]) | uint32(ip[2])<<8 | uint32(ip[1])<<16 | uint32(ip[0])<<24
}

// ToNetIP converts a uint32 to a net.IP (net.IPv4 actually)
func ToNetIP(val uint32) string {
	return net.IPv4(byte(val>>24), byte(val>>16&0xFF),
		byte(val>>8)&0xFF, byte(val&0xFF)).String()
}
