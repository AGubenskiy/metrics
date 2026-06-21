package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AGubenskiy/metrics/internal/agent"
	"github.com/AGubenskiy/metrics/internal/buildinfo"
	appconfig "github.com/AGubenskiy/metrics/internal/config"
	"github.com/AGubenskiy/metrics/internal/cryptoutil"
	pb "github.com/AGubenskiy/metrics/internal/proto"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	buildVersion = "N/A"
	buildDate    = "N/A"
	buildCommit  = "N/A"
)

func main() {
	if err := buildinfo.Print(os.Stdout, buildVersion, buildDate, buildCommit); err != nil {
		log.Printf("cannot print build info: %v", err)
	}

	const (
		defaultAddr           = "localhost:8080"
		defaultGRPCAddr       = ""
		defaultReportInterval = 10
		defaultPollInterval   = 2
		defaultRateLimit      = 1
	)

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	addr := flag.String("a", defaultAddr, "server address")
	grpcAddr := flag.String("grpc-a", defaultGRPCAddr, "gRPC server address")
	flag.StringVar(grpcAddr, "grpc-address", defaultGRPCAddr, "gRPC server address")
	reportInterval := flag.Int("r", defaultReportInterval, "report interval in seconds")
	pollInterval := flag.Int("p", defaultPollInterval, "poll interval in seconds")
	rateLimit := flag.Int("l", defaultRateLimit, "max out requests")
	key := flag.String("k", "", "hash key")
	cryptoKey := flag.String("crypto-key", "", "path to public crypto key")
	configPath := ""
	flag.StringVar(&configPath, "c", "", "path to JSON config file")
	flag.StringVar(&configPath, "config", "", "path to JSON config file")
	flag.Parse()

	setFlags := map[string]bool{}
	flag.CommandLine.Visit(func(f *flag.Flag) {
		setFlags[f.Name] = true
	})

	finalConfigPath := appconfig.ResolveString([]string{"CONFIG"}, setFlags["c"] || setFlags["config"], configPath, nil, "")
	fileConfig, err := appconfig.LoadAgent(finalConfigPath)
	if err != nil {
		log.Fatal(err)
	}

	finalAddr := appconfig.ResolveString([]string{"ADDRESS"}, setFlags["a"], *addr, fileConfig.Address, defaultAddr)
	finalGRPCAddr := appconfig.ResolveString(
		[]string{"GRPC_ADDRESS"},
		setFlags["grpc-a"] || setFlags["grpc-address"],
		*grpcAddr,
		fileConfig.GRPCAddress,
		defaultGRPCAddr,
	)

	finalReportInterval, err := appconfig.ResolveSeconds([]string{"REPORT_INTERVAL"}, setFlags["r"], *reportInterval, fileConfig.ReportInterval, defaultReportInterval)
	if err != nil {
		log.Fatal(err)
	}
	if finalReportInterval <= 0 {
		log.Fatalf("report interval must be positive, got %d", finalReportInterval)
	}

	finalPollInterval, err := appconfig.ResolveSeconds([]string{"POLL_INTERVAL"}, setFlags["p"], *pollInterval, fileConfig.PollInterval, defaultPollInterval)
	if err != nil {
		log.Fatal(err)
	}
	if finalPollInterval <= 0 {
		log.Fatalf("poll interval must be positive, got %d", finalPollInterval)
	}

	finalRateLimit, err := appconfig.ResolveInt([]string{"RATE_LIMIT"}, setFlags["l"], *rateLimit, fileConfig.RateLimit, defaultRateLimit)
	if err != nil {
		log.Fatal(err)
	}

	if finalRateLimit <= 0 {
		log.Fatalf("rate limit must be positive, got %d", finalRateLimit)
	}

	finalKey := appconfig.ResolveString([]string{"KEY"}, setFlags["k"], *key, fileConfig.Key, "")
	finalCryptoKey := appconfig.ResolveString([]string{"CRYPTO_KEY"}, setFlags["crypto-key"], *cryptoKey, fileConfig.CryptoKey, "")

	log.Printf(
		"Agent started: addr=http://%s, grpc_addr=%s, report=%v, poll=%v, rate_limit=%v, crypto=%t",
		finalAddr,
		finalGRPCAddr,
		finalReportInterval,
		finalPollInterval,
		finalRateLimit,
		finalCryptoKey != "",
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
	if finalGRPCAddr != "" {
		dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
		grpcConn, err := grpc.DialContext(
			dialCtx,
			finalGRPCAddr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithBlock(),
		)
		dialCancel()
		if err != nil {
			log.Fatalf("cannot connect to gRPC server: %v", err)
		}
		defer func() {
			if closeErr := grpcConn.Close(); closeErr != nil {
				log.Printf("cannot close gRPC connection: %v", closeErr)
			}
		}()
		a.SetGRPCClient(pb.NewMetricsClient(grpcConn), finalGRPCAddr)
	}
	if finalCryptoKey != "" {
		publicKey, err := cryptoutil.LoadPublicKey(finalCryptoKey)
		if err != nil {
			log.Fatalf("cannot load public crypto key: %v", err)
		}
		a.SetEncryptionPublicKey(publicKey)
	}
	a.SetLogger(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)
	defer stop()

	a.Run(ctx)
}
