package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/brettmitchelldev/redfi"
)

var (
	planPath = flag.String("plan", "", "Path to the plan file, must be formatted as JSON")
	server   = flag.String("redis", "127.0.0.1:6379", "Address of the target Redis server, to proxy requests to")
	addr     = flag.String("addr", "127.0.0.1:8083", "Address for the proxy to listen on")
	apiAddr  = flag.String("api", "127.0.0.1:8081", "Address for the HTTP API to listen on")
	logging  = flag.String("log", "", "Log level (give 'v' for verbose logging, 'vv' for very verbose)")
)

func main() {
	flag.Parse()

	proxy, err := redfi.New(*planPath, *server, *addr, *apiAddr, *logging)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	logger := redfi.MakeLogger(len(*logging))

	go func() {
		proxy.StartAPI()
	}()
	proxy.Start(logger)
}
