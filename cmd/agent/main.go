package main

import (
	"flag"
	"github.com/AGubenskiy/metrics/internal/agent"
	"log"
	"os"
)

func main() {
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	addr := flag.String("a", "localhost:8080", "server address")
	reportInterval := flag.Int("r", 10, "report interval in seconds")
	pollInterval := flag.Int("p", 2, "poll interval in seconds")
	flag.Parse()
	log.Printf(
		"Agent started: addr=http://%s, report=%v, poll=%v",
		*addr,
		*reportInterval,
		*pollInterval,
	)
	a := agent.NewAgent("http://"+*addr, *reportInterval, *pollInterval)
	a.Run()
}
