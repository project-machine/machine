package api

import (
	"fmt"
	"net"
)

func portAvail(p int) bool {

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", p))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

func NextFreePort(first int) int {
	for p := first; ; p++ {
		if portAvail(p) {
			return p
		}
	}
}
