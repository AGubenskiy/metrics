package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strconv"

	"github.com/AGubenskiy/metrics/internal/agent"
	"github.com/AGubenskiy/metrics/internal/buildinfo"
	"go.uber.org/zap"
)

var (
	buildVersion string
	buildDate    string
	buildCommit  string
)

func main() {
	if err := buildinfo.Print(os.Stdout, buildVersion, buildDate, buildCommit); err != nil {
		log.Printf("cannot print build info: %v", err)
	}

	const (
		defaultAddr           = "localhost:8080"
		defaultReportInterval = 10
		defaultPollInterval   = 2
		defaultRateLimit      = 1
	)

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	addr := flag.String("a", defaultAddr, "server address")
	reportInterval := flag.Int("r", defaultReportInterval, "report interval in seconds")
	pollInterval := flag.Int("p", defaultPollInterval, "poll interval in seconds")
	rateLimit := flag.Int("l", defaultRateLimit, "max out requests")
	key := flag.String("k", "", "hash key")
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

	finalRateLimit := defaultRateLimit
	if envRateLimit := os.Getenv("RATE_LIMIT"); envRateLimit != "" {
		parsedRateLimit, err := strconv.Atoi(envRateLimit)
		if err != nil {
			log.Fatalf("invalid RATE_LIMIT value %q: %v", envRateLimit, err)
		}
		finalRateLimit = parsedRateLimit
	} else if setFlags["l"] {
		finalRateLimit = *rateLimit
	}

	if finalRateLimit <= 0 {
		log.Fatalf("rate limit must be positive, got %d", finalRateLimit)
	}

	finalKey := ""
	if envKey := os.Getenv("KEY"); envKey != "" {
		finalKey = envKey
	} else if setFlags["k"] {
		finalKey = *key
	}

	log.Printf(
		"Agent started: addr=http://%s, report=%v, poll=%v, rate_limit=%v",
		finalAddr,
		finalReportInterval,
		finalPollInterval,
		finalRateLimit,
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

	a := agent.NewAgentWithRateLimit("http://"+finalAddr, finalReportInterval, finalPollInterval, finalRateLimit, finalKey)
	a.SetLogger(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	a.Run(ctx)
}
