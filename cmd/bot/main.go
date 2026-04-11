package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"time"

	"codeberg.org/btsemm/btsemm"
)

var Commit = func() string {
	ret := ""
	dirty := ""
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.modified" {
				if setting.Value == "true" {
					dirty = "dirty"
				} else {
					dirty = "clean"
				}
			}
			if setting.Key == "vcs.revision" {
				ret = setting.Value[0:7]
			}
		}
	}
	return ret + "-" + dirty
}()

var FullVersion = func() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		var rev, vcsTime, dirty string
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.time":
				vcsTime = s.Value
			case "vcs.modified":
				if s.Value == "true" {
					dirty = "dirty"
				} else {
					dirty = "clean"
				}
			}
		}
		return fmt.Sprintf("%s %s built=%s go=%s", rev, dirty, vcsTime, info.GoVersion)
	}
	return Commit
}()

// Global flags
var (
	// Engine
	flagSymbol    = flag.String("symbol", "BTC-USDT", "trading pair")
	flagStrategy  = flag.String("strategy", "simple", "strategy name: "+availableStrategies())
	flagTick      = flag.Duration("tick", 1*time.Second, "tick interval")
	flagReconcile = flag.Duration("reconcile", 30*time.Second, "reconcile interval")
	flagDeadMan   = flag.Int64("deadman", 30000, "dead man's switch timeout in ms (0=off)")
	flagMaxErrors = flag.Int("max-errors", 5, "kill after N consecutive order errors")
	flagAmendBPS  = flag.Float64("amend-bps", 1, "min price move to replace orders, in basis points of order price (1 = 0.01%)")
	flagTestnet     = flag.Bool("testnet", false, "use BTSE testnet")
	flagVersion     = flag.Bool("version", false, "print short version and exit")
	flagFullVersion = flag.Bool("fullversion", false, "print full version and exit")

	// Risk
	flagMaxPos  = flag.Float64("risk.max-pos", 100, "max position in USDT")
	flagMaxLoss = flag.Float64("risk.max-loss", 10, "max loss in USDT before kill")
	flagMaxOrd  = flag.Int("risk.max-orders", 10, "max open orders")
	flagDataTTL = flag.Duration("risk.data-ttl", 30*time.Second, "kill if no data for this long")
)

func init() {
	data, _ := os.ReadFile(".env")
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		if k, v, ok := strings.Cut(line, "="); ok {
			os.Setenv(k, strings.Trim(v, `"`))
		}
	}
}

func main() {
	flag.Parse()

	if *flagVersion {
		fmt.Println(Commit)
		return
	}
	if *flagFullVersion {
		fmt.Println(FullVersion)
		return
	}

	// Set up log file + stdout
	logName := fmt.Sprintf("bot-%s.log", time.Now().Format("2006-01-02-15-04"))
	logFile, err := os.OpenFile(logName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("open log file: %v", err)
	}
	defer logFile.Close()
	log.SetOutput(io.MultiWriter(os.Stdout, logFile))
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	log.Printf("=== bot %s ===", Commit)
	log.Printf("=== cmd: %s ===", strings.Join(os.Args, " "))

	// Look up strategy
	ctor, ok := strategies[*flagStrategy]
	if !ok {
		log.Fatalf("unknown strategy %q — available: %s", *flagStrategy, availableStrategies())
	}
	strategy := ctor()

	log.Printf("=== bot starting symbol=%s strategy=%s ===", *flagSymbol, *flagStrategy)

	var opts []btsemm.Option
	if *flagTestnet {
		opts = append(opts, btsemm.WithTestnet())
		log.Printf("=== TESTNET MODE ===")
		if *flagDeadMan > 0 {
			log.Printf("WARNING: dead man's switch may not work on testnet")
		}
	}
	client := btsemm.NewFromEnv(opts...)

	config := btsemm.EngineConfig{
		Symbol:            *flagSymbol,
		TickInterval:      *flagTick,
		ReconcileInterval: *flagReconcile,
		DeadManTimeout:    *flagDeadMan,
		MaxConsecErrors:   *flagMaxErrors,
		AmendBPS:          *flagAmendBPS,
		Risk: btsemm.RiskConfig{
			MaxPositionUSDT:  *flagMaxPos,
			MaxLossUSDT:      *flagMaxLoss,
			MaxOpenOrders:    *flagMaxOrd,
			KillOnDisconnect: *flagDataTTL,
		},
	}

	engine := btsemm.NewEngine(client, config, strategy)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		<-sig
		fmt.Println("\nShutting down...")
		cancel()
	}()

	if err := engine.Run(ctx); err != nil {
		log.Fatalf("engine: %v", err)
	}
	log.Printf("=== bot stopped ===")
}

func availableStrategies() string {
	names := make([]string, 0, len(strategies))
	for k := range strategies {
		names = append(names, k)
	}
	return strings.Join(names, ", ")
}
