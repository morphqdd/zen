package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ServerConfig конфигурация сервера
type ServerConfig struct {
	Domain         string `json:"domain"`
	ListenAddr     string `json:"listen_addr"`
	EncryptionKey  string `json:"encryption_key"`  // hex
	HMACSecret     string `json:"hmac_secret"`     // hex
	TUNName        string `json:"tun_name"`
	ServerIP       string `json:"server_ip"`
	MTU            string `json:"mtu"`
	NextClientID   int    `json:"next_client_id"`
}

// ClientConfig конфигурация клиента
type ClientConfig struct {
	ClientID       int    `json:"client_id"`
	ClientName     string `json:"client_name"`
	Domain         string `json:"domain"`
	DoHServer      string `json:"doh_server"`
	EncryptionKey  string `json:"encryption_key"`  // hex
	HMACSecret     string `json:"hmac_secret"`     // hex
	ClientIP       string `json:"client_ip"`
	SubdomainStyle string `json:"subdomain_style"`
	MTU            string `json:"mtu"`
}

const (
	DefaultConfigDir    = "/etc/zen"
	DefaultServerConfig = "server.conf"
	DefaultClientsDir   = "clients"
)

// LoadServerConfig загружает конфигурацию сервера
func LoadServerConfig(path string) (*ServerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config ServerConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// SaveServerConfig сохраняет конфигурацию сервера
func (c *ServerConfig) Save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	// Создаём директорию если не существует
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

// LoadClientConfig загружает конфигурацию клиента
func LoadClientConfig(path string) (*ClientConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config ClientConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// Save сохраняет конфигурацию клиента
func (c *ClientConfig) Save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	// Создаём директорию если не существует
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

// ExportForClient создаёт упрощённый конфиг для клиента (без client_id)
func (c *ClientConfig) ExportForClient() string {
	export := fmt.Sprintf(`# Zen VPN Client Configuration
# Client: %s

domain = "%s"
doh_server = "%s"
encryption_key = "%s"
hmac_secret = "%s"
client_ip = "%s"
subdomain_style = "%s"
mtu = "%s"
`,
		c.ClientName,
		c.Domain,
		c.DoHServer,
		c.EncryptionKey,
		c.HMACSecret,
		c.ClientIP,
		c.SubdomainStyle,
		c.MTU,
	)

	return export
}

// GenerateKeys генерирует случайные ключи шифрования
func GenerateKeys() (encryptionKey, hmacSecret string, err error) {
	// 32 байта для ChaCha20-Poly1305
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return "", "", fmt.Errorf("failed to generate encryption key: %w", err)
	}

	// 32 байта для HMAC
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return "", "", fmt.Errorf("failed to generate HMAC secret: %w", err)
	}

	return hex.EncodeToString(keyBytes), hex.EncodeToString(secretBytes), nil
}

// GetClientConfigPath возвращает путь к конфигу клиента
func GetClientConfigPath(configDir, clientName string) string {
	return filepath.Join(configDir, DefaultClientsDir, clientName+".json")
}

// GetServerConfigPath возвращает путь к конфигу сервера
func GetServerConfigPath(configDir string) string {
	return filepath.Join(configDir, DefaultServerConfig)
}

// ListClients возвращает список всех клиентов
func ListClients(configDir string) ([]string, error) {
	clientsDir := filepath.Join(configDir, DefaultClientsDir)

	entries, err := os.ReadDir(clientsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	var clients []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			name := entry.Name()[:len(entry.Name())-5] // remove .json
			clients = append(clients, name)
		}
	}

	return clients, nil
}
