package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

func main() {
	configPath := flag.String("config", "", "Path to config.json (optional - will update file if provided)")
	output := flag.String("output", "", "Output file for the key (optional)")
	flag.Parse()

	// Generate 32 random bytes
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		log.Fatalf("Failed to generate random bytes: %v", err)
	}

	// Encode as hex for safe storage in JSON/config files
	key := hex.EncodeToString(keyBytes)

	fmt.Println("=== New Encryption Key Generated ===")
	fmt.Printf("Key (hex): %s\n", key)
	fmt.Printf("Key length: %d bytes\n", len(keyBytes))
	fmt.Println()

	// Save to file if requested
	if *output != "" {
		if err := os.WriteFile(*output, []byte(key), 0600); err != nil {
			log.Fatalf("Failed to write key to file: %v", err)
		}
		fmt.Printf("Key saved to: %s\n", *output)
	}

	// Update config.json if path provided
	if *configPath != "" {
		if err := updateConfigFile(*configPath, key); err != nil {
			log.Fatalf("Failed to update config: %v", err)
		}
		fmt.Printf("Config updated: %s\n", *configPath)
	}

	fmt.Println()
	fmt.Println("=== IMPORTANT ===")
	fmt.Println("1. Store this key securely - it cannot be recovered if lost")
	fmt.Println("2. All existing encrypted passwords will need to be re-entered")
	fmt.Println("3. Back up your database before rotating keys in production")
	fmt.Println("4. Set file permissions: chmod 600 config.json")
}

func updateConfigFile(configPath, key string) error {
	// Resolve to absolute path
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	// Read existing config
	data, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	// Parse as generic JSON
	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	// Update or create security.encryption_key
	security, ok := config["security"].(map[string]interface{})
	if !ok {
		security = make(map[string]interface{})
		config["security"] = security
	}
	security["encryption_key"] = key

	// Marshal back to JSON with indentation
	output, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Backup existing config with timestamp
	timestamp := time.Now().Format("20060102_150405")
	backupPath := fmt.Sprintf("%s.%s.bak", absPath, timestamp)
	if err := os.Rename(absPath, backupPath); err != nil {
		return fmt.Errorf("failed to backup config: %w", err)
	}
	fmt.Printf("Backup created: %s\n", backupPath)

	// Write new config
	if err := os.WriteFile(absPath, output, 0600); err != nil {
		// Restore backup on failure
		os.Rename(backupPath, absPath)
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}
