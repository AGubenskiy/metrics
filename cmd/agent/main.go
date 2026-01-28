package main

import "github.com/AGubenskiy/metrics/internal/agent"

func main() {
	a := agent.NewAgent("http://localhost:8080")
	a.Run()
}
