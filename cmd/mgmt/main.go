package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"text/tabwriter"
	"webmail_engine/internal/config"
	"webmail_engine/internal/store"
)

func main() {
	configPath := flag.String("config", "./config/config.json", "Path to config file")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		printUsage()
		return
	}

	cfg, err := config.LoadFromFile(*configPath)
	if err != nil {
		log.Printf("Warning: Failed to load config from %s, using defaults and environment: %v", *configPath, err)
		cfg = config.LoadFromEnv()
	}

	var accountStore store.AccountStore
	switch cfg.Store.Type {
	case "sql":
		if cfg.Store.SQL == nil {
			cfg.Store.SQL = &config.SQLConfig{Driver: "sqlite", DSN: "./data/accounts.db", MaxConnections: 10}
		}
		log.Printf("Using SQL store with driver=%s, dsn=%s", cfg.Store.SQL.Driver, cfg.Store.SQL.DSN)
		accountStore, err = store.NewSQLStore(*cfg.Store.SQL)
		if err != nil {
			log.Fatalf("Failed to initialize SQL store: %v", err)
		}
	case "memory":
		accountStore = store.NewMemoryStore()
	default:
		log.Fatalf("Unknown store type: %s", cfg.Store.Type)
	}
	defer accountStore.Close()

	ctx := context.Background()

	command := args[0]
	switch command {
	case "list-accounts":
		listAccounts(ctx, accountStore)
	case "list-table":
		listTable(ctx, accountStore)
	case "log-attempts":
		logAttempts(ctx, accountStore)
	case "migrate":
		runMigrations(ctx, accountStore)
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printUsage()
	}
}

func printUsage() {
	fmt.Println("Usage: mgmt <command> [options]")
	fmt.Println("\nCommands:")
	fmt.Println("  list-accounts    List summary of email accounts")
	fmt.Println("  list-table       List detailed table of accounts")
	fmt.Println("  log-attempts     List failed login audit logs")
	fmt.Println("  migrate          Run database migrations (apply schema changes and indexes)")
}

func listAccounts(ctx context.Context, s store.AccountStore) {
	accounts, _, err := s.List(ctx, 0, 1000)
	if err != nil {
		log.Fatalf("Failed to list accounts: %v", err)
	}

	fmt.Printf("Total Accounts: %d\n\n", len(accounts))
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tEMAIL\tSTATUS\tCREATED")
	for _, acc := range accounts {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			acc.ID, acc.Email, acc.Status, acc.CreatedAt.Format("2006-01-02 15:04:05"))
	}
	w.Flush()
}

func listTable(ctx context.Context, s store.AccountStore) {
	accounts, _, err := s.List(ctx, 0, 1000)
	if err != nil {
		log.Fatalf("Failed to list accounts: %v", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', tabwriter.Debug)
	fmt.Fprintln(w, "ID|EMAIL|AUTH|STATUS|IMAP_HOST|SMTP_HOST|CREATED")
	for _, acc := range accounts {
		fmt.Fprintf(w, "%s|%s|%s|%s|%s|%s|%s\n",
			acc.ID, acc.Email, acc.AuthType, acc.Status,
			acc.IMAPConfig.Host, acc.SMTPConfig.Host,
			acc.CreatedAt.Format("2006-01-02 15:04:05"))
	}
	w.Flush()
}

func logAttempts(ctx context.Context, s store.AccountStore) {
	logs, _, err := s.ListAuditLogs(ctx, 0, 100)
	if err != nil {
		log.Fatalf("Failed to list audit logs: %v", err)
	}

	fmt.Printf("Recent Failed Login logs (Last 100):\n\n")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TIME\tACCOUNT\tEMAIL\tEVENT\tDETAILS\tIP")
	for _, l := range logs {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			l.Timestamp.Format("2006-01-02 15:04:05"), l.AccountID, l.Email, l.Event, l.Details, l.IP)
	}
	w.Flush()
}

func runMigrations(ctx context.Context, s store.AccountStore) {
	log.Println("Running database migrations...")

	if err := s.RunManualMigrations(); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	log.Println("Migrations completed successfully!")
	log.Println("Applied schema changes and indexes:")
	log.Println("  - idx_folder_sync_account_folder (folder_sync_states)")
	log.Println("  - idx_accounts_status (accounts)")
	log.Println("  - idx on last_sync_time (folder_sync_states)")
	log.Println("  - idx on updated_at (folder_sync_states)")
	log.Println("  - idx on created_at (accounts)")
	log.Println("  - idx on updated_at (accounts)")
	log.Println("  - idx on last_sync_at (accounts)")
}
