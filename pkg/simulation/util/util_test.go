package util

import (
	"net"
	"testing"
)

func TestGetIP(t *testing.T) {
	nextIp = net.ParseIP("10.0.0.10")

	ips := map[string]bool{}
	for i := 0; i < 10000; i++ {
		ip := GetIP()
		if _, ok := ips[ip]; ok {
			t.Fatalf("duplicate %s", ip)
		}
		ips[ip] = true
	}

}
