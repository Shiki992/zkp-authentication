package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"strconv"

	"github.com/srinathLN7/zkp_auth/cmd"
	cp_zkp "github.com/srinathLN7/zkp_auth/internal/cpzkp"
	"github.com/srinathLN7/zkp_auth/internal/database"
	"github.com/srinathLN7/zkp_auth/internal/server"
)

func init() {
	cmd.SetupFlags()
}

func main() {

	var runServerInBackground = flag.Bool("server", false, "run grpc server in the background")
	flag.Parse()

	// Check if the --server flag is set
	if *runServerInBackground {
		cpzkpParams, err := cp_zkp.NewCPZKP()
		if err != nil {
			log.Fatal("error generating system parameters:", err)
		}

		// Initialize DB from environment (optional - defaults provided)
		dbPort := 5432
		if p := os.Getenv("DB_PORT"); p != "" {
			if v, err := strconv.Atoi(p); err == nil {
				dbPort = v
			}
		}

		dbCfg := database.Config{
			Host:     getenvOrDefault("DB_HOST", "localhost"),
			Port:     dbPort,
			User:     getenvOrDefault("DB_USER", "postgres"),
			Password: getenvOrDefault("DB_PASSWORD", "mdl"),
			DBName:   getenvOrDefault("DB_NAME", "zkp_auth"),
			SSLMode:  getenvOrDefault("DB_SSLMODE", "disable"),
		}

		db, err := database.NewDatabase(dbCfg)
		if err != nil {
			// If DB is not available, log and continue with in-memory mode (handlers will fail gracefully)
			log.Printf("warning: failed to initialize database, running without DB: %v", err)
			db = nil
		}

		cfg := &server.Config{
			CPZKP: cpzkpParams,
			DB:    db,
		}

		// Create and start the gRPC server in the background
		// To do this, we spin up a new go routine
		go server.RunServer(cfg)

		// Wait for a graceful shutdown signal (e.g., Ctrl+C)
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		<-c

		// Close DB if initialized
		if cfg.DB != nil {
			if err := cfg.DB.Close(); err != nil {
				log.Printf("error closing database: %v", err)
			}
		}

		// If the server is running, return to prevent executing Cobra commands
		return
	}

	//  Execute the Cobra commands otherwise
	if err := cmd.RootCmd.Execute(); err != nil {
		log.Fatal("error:", err)
	}
}

func getenvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
