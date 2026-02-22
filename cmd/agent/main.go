package main

import (
	"context"
	"flag"
	"github.com/AGubenskiy/metrics/internal/agent"
	"log"
	"os"
	"os/signal"
	"strconv"

	"go.uber.org/zap"
)

func main() {
	const (
		defaultAddr           = "localhost:8080"
		defaultReportInterval = 10
		defaultPollInterval   = 2
	)

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	addr := flag.String("a", defaultAddr, "server address")
	reportInterval := flag.Int("r", defaultReportInterval, "report interval in seconds")
	pollInterval := flag.Int("p", defaultPollInterval, "poll interval in seconds")
	flag.Parse()

	setFlags := map[string]bool{}
	flag.CommandLine.Visit(func(f *flag.Flag) {
		setFlags[f.Name] = true
	})

	finalAddr := defaultAddr
	if envAddr := os.Getenv("ADDRESS"); envAddr != "" {
		finalAddr = envAddr
	} else if setFlags["a"] {
		finalAddr = *addr
	}

	finalReportInterval := defaultReportInterval
	if envReportInterval := os.Getenv("REPORT_INTERVAL"); envReportInterval != "" {
		parsedReportInterval, err := strconv.Atoi(envReportInterval)
		if err != nil {
			log.Fatalf("invalid REPORT_INTERVAL value %q: %v", envReportInterval, err)
		}
		finalReportInterval = parsedReportInterval
	} else if setFlags["r"] {
		finalReportInterval = *reportInterval
	}

	finalPollInterval := defaultPollInterval
	if envPollInterval := os.Getenv("POLL_INTERVAL"); envPollInterval != "" {
		parsedPollInterval, err := strconv.Atoi(envPollInterval)
		if err != nil {
			log.Fatalf("invalid POLL_INTERVAL value %q: %v", envPollInterval, err)
		}
		finalPollInterval = parsedPollInterval
	} else if setFlags["p"] {
		finalPollInterval = *pollInterval
	}

	log.Printf(
		"Agent started: addr=http://%s, report=%v, poll=%v",
		finalAddr,
		finalReportInterval,
		finalPollInterval,
	)

	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("cannot initialize logger: %v", err)
	}
	defer func() {
		if syncErr := logger.Sync(); syncErr != nil {
			log.Printf("logger sync error: %v", syncErr)
		}
	}()

	a := agent.NewAgent("http://"+finalAddr, finalReportInterval, finalPollInterval)
	a.SetLogger(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	a.Run(ctx)
}
