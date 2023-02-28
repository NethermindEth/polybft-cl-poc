package main

import (
	"github.com/0xPolygon/polygon-edge/network"
	"github.com/hashicorp/go-hclog"
)

func main() {
	server, err := network.NewServer(hclog.Default(), network.DefaultConfig())
	if err != nil {
		panic(err)
	}

}
