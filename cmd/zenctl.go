package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"zen/internal/config"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "init":
		initCmd()
	case "add-client":
		addClientCmd()
	case "list-clients":
		listClientsCmd()
	case "show-client":
		showClientCmd()
	case "remove-client":
		removeClientCmd()
	case "version":
		fmt.Printf("zenctl version %s\n", version)
	case "help":
		printUsage()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Printf(`zenctl - Zen VPN configuration manager

Usage:
  zenctl <command> [options]

Commands:
  init                          Initialize server configuration
  add-client <name>            Add a new client
  list-clients                 List all clients
  show-client <name>           Show client configuration
  remove-client <name>         Remove a client
  version                      Show version
  help                         Show this help

Examples:
  # Initialize server
  sudo zenctl init --domain vpn.example.com

  # Add client
  sudo zenctl add-client alice

  # List all clients
  sudo zenctl list-clients

  # Show client config (for importing to client)
  sudo zenctl show-client alice

  # Remove client
  sudo zenctl remove-client alice
`)
}

func initCmd() {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	domain := fs.String("domain", "", "Base domain for VPN (required)")
	listenAddr := fs.String("listen", ":53", "DNS server listen address")
	configDir := fs.String("config-dir", config.DefaultConfigDir, "Configuration directory")

	fs.Parse(os.Args[2:])

	if *domain == "" {
		log.Fatalln("Domain is required: --domain vpn.example.com")
	}

	// Проверяем что конфиг ещё не существует
	serverConfigPath := config.GetServerConfigPath(*configDir)
	if _, err := os.Stat(serverConfigPath); err == nil {
		log.Fatalf("Server config already exists: %s\nUse 'zenctl reset' to reinitialize", serverConfigPath)
	}

	// Генерируем ключи
	fmt.Println("Generating encryption keys...")
	encKey, hmacSecret, err := config.GenerateKeys()
	if err != nil {
		log.Fatalf("Failed to generate keys: %v", err)
	}

	// Создаём конфиг сервера
	serverConfig := &config.ServerConfig{
		Domain:        *domain,
		ListenAddr:    *listenAddr,
		EncryptionKey: encKey,
		HMACSecret:    hmacSecret,
		TUNName:       "zen-srv",
		ServerIP:      "10.0.0.254/24",
		MTU:           "1300",
		NextClientID:  1,
	}

	if err := serverConfig.Save(serverConfigPath); err != nil {
		log.Fatalf("Failed to save server config: %v", err)
	}

	fmt.Printf("✓ Server initialized successfully!\n\n")
	fmt.Printf("Configuration saved to: %s\n\n", serverConfigPath)
	fmt.Printf("Start server with:\n")
	fmt.Printf("  sudo zen-server --config %s\n\n", serverConfigPath)
	fmt.Printf("Add clients with:\n")
	fmt.Printf("  sudo zenctl add-client <client-name>\n")
}

func addClientCmd() {
	if len(os.Args) < 3 {
		log.Fatalln("Usage: zenctl add-client <name>")
	}

	fs := flag.NewFlagSet("add-client", flag.ExitOnError)
	configDir := fs.String("config-dir", config.DefaultConfigDir, "Configuration directory")
	dohServer := fs.String("doh", "https://dns.yandex.ru/dns-query", "DoH server for client")
	style := fs.String("style", "mixed", "Subdomain style: api, cdn, storage, mixed")

	fs.Parse(os.Args[3:])

	clientName := os.Args[2]

	// Загружаем серверную конфигурацию
	serverConfigPath := config.GetServerConfigPath(*configDir)
	serverConfig, err := config.LoadServerConfig(serverConfigPath)
	if err != nil {
		log.Fatalf("Failed to load server config: %v\nRun 'zenctl init' first", err)
	}

	// Проверяем что клиент не существует
	clientConfigPath := config.GetClientConfigPath(*configDir, clientName)
	if _, err := os.Stat(clientConfigPath); err == nil {
		log.Fatalf("Client '%s' already exists", clientName)
	}

	// Вычисляем IP для клиента
	clientIP := fmt.Sprintf("10.0.0.%d/24", serverConfig.NextClientID)

	// Создаём конфиг клиента
	clientConfig := &config.ClientConfig{
		ClientID:       serverConfig.NextClientID,
		ClientName:     clientName,
		Domain:         serverConfig.Domain,
		DoHServer:      *dohServer,
		EncryptionKey:  serverConfig.EncryptionKey,
		HMACSecret:     serverConfig.HMACSecret,
		ClientIP:       clientIP,
		SubdomainStyle: *style,
		MTU:            serverConfig.MTU,
	}

	if err := clientConfig.Save(clientConfigPath); err != nil {
		log.Fatalf("Failed to save client config: %v", err)
	}

	// Обновляем счётчик клиентов в серверной конфигурации
	serverConfig.NextClientID++
	if err := serverConfig.Save(serverConfigPath); err != nil {
		log.Fatalf("Failed to update server config: %v", err)
	}

	fmt.Printf("✓ Client '%s' added successfully!\n\n", clientName)
	fmt.Printf("Configuration saved to: %s\n\n", clientConfigPath)
	fmt.Printf("Client can connect with:\n")
	fmt.Printf("  sudo zen-client --config %s\n\n", clientConfigPath)
	fmt.Printf("Or show portable config:\n")
	fmt.Printf("  sudo zenctl show-client %s\n", clientName)
}

func listClientsCmd() {
	fs := flag.NewFlagSet("list-clients", flag.ExitOnError)
	configDir := fs.String("config-dir", config.DefaultConfigDir, "Configuration directory")
	fs.Parse(os.Args[2:])

	clients, err := config.ListClients(*configDir)
	if err != nil {
		log.Fatalf("Failed to list clients: %v", err)
	}

	if len(clients) == 0 {
		fmt.Println("No clients configured")
		return
	}

	fmt.Printf("Configured clients (%d):\n\n", len(clients))
	for i, client := range clients {
		clientConfigPath := config.GetClientConfigPath(*configDir, client)
		clientConfig, err := config.LoadClientConfig(clientConfigPath)
		if err != nil {
			fmt.Printf("%d. %s (error loading config)\n", i+1, client)
			continue
		}

		fmt.Printf("%d. %s\n", i+1, client)
		fmt.Printf("   IP: %s\n", clientConfig.ClientIP)
		fmt.Printf("   Style: %s\n", clientConfig.SubdomainStyle)
		fmt.Printf("   DoH: %s\n", clientConfig.DoHServer)
	}
}

func showClientCmd() {
	if len(os.Args) < 3 {
		log.Fatalln("Usage: zenctl show-client <name>")
	}

	fs := flag.NewFlagSet("show-client", flag.ExitOnError)
	configDir := fs.String("config-dir", config.DefaultConfigDir, "Configuration directory")
	format := fs.String("format", "text", "Output format: text, json, qr")

	fs.Parse(os.Args[3:])

	clientName := os.Args[2]

	clientConfigPath := config.GetClientConfigPath(*configDir, clientName)
	clientConfig, err := config.LoadClientConfig(clientConfigPath)
	if err != nil {
		log.Fatalf("Failed to load client config: %v", err)
	}

	switch *format {
	case "json":
		// Экспортируем только необходимые поля (без client_id)
		export := map[string]string{
			"domain":          clientConfig.Domain,
			"doh_server":      clientConfig.DoHServer,
			"encryption_key":  clientConfig.EncryptionKey,
			"hmac_secret":     clientConfig.HMACSecret,
			"client_ip":       clientConfig.ClientIP,
			"subdomain_style": clientConfig.SubdomainStyle,
			"mtu":             clientConfig.MTU,
		}
		jsonData, err := json.MarshalIndent(export, "", "  ")
		if err != nil {
			log.Fatalf("Failed to marshal JSON: %v", err)
		}
		fmt.Println(string(jsonData))

	case "qr":
		fmt.Println("QR code generation not implemented yet")
		fmt.Println("Use 'zenctl show-client <name> --format text' to get configuration")

	default: // text
		fmt.Printf("# Configuration for client: %s\n\n", clientName)
		fmt.Println(clientConfig.ExportForClient())
		fmt.Printf("\nConnect with:\n")
		fmt.Printf("  sudo zen-client \\\n")
		fmt.Printf("    --domain %s \\\n", clientConfig.Domain)
		fmt.Printf("    --doh %s \\\n", clientConfig.DoHServer)
		fmt.Printf("    --key %s \\\n", clientConfig.EncryptionKey)
		fmt.Printf("    --secret %s \\\n", clientConfig.HMACSecret)
		fmt.Printf("    --style %s\n", clientConfig.SubdomainStyle)
	}
}

func removeClientCmd() {
	if len(os.Args) < 3 {
		log.Fatalln("Usage: zenctl remove-client <name>")
	}

	fs := flag.NewFlagSet("remove-client", flag.ExitOnError)
	configDir := fs.String("config-dir", config.DefaultConfigDir, "Configuration directory")
	fs.Parse(os.Args[3:])

	clientName := os.Args[2]

	clientConfigPath := config.GetClientConfigPath(*configDir, clientName)
	if _, err := os.Stat(clientConfigPath); os.IsNotExist(err) {
		log.Fatalf("Client '%s' does not exist", clientName)
	}

	if err := os.Remove(clientConfigPath); err != nil {
		log.Fatalf("Failed to remove client config: %v", err)
	}

	fmt.Printf("✓ Client '%s' removed successfully\n", clientName)
}
