package main

import (
	"log"
	"os"
)

var _ = helper

func main() {
	if len(os.Args) == 0 {
		log.Fatal("allowed")
	}
	if len(os.Args) == 0 {
		os.Exit(0)
	}

	func() {
		if len(os.Args) == 0 {
			log.Fatal("forbidden") // want "log\\.Fatal must only be called from main\\.main"
		}
		if len(os.Args) == 0 {
			os.Exit(1) // want "os\\.Exit must only be called from main\\.main"
		}
	}()
}

func helper() {
	if len(os.Args) == 0 {
		log.Fatal("forbidden") // want "log\\.Fatal must only be called from main\\.main"
	}
	if len(os.Args) == 0 {
		os.Exit(1) // want "os\\.Exit must only be called from main\\.main"
	}
}
