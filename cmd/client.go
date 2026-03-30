package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"zen/internal/config"
	"zen/internal/crypto"
	"zen/internal/doh"
	"zen/internal/encoding"
	"zen/internal/utils"

	"github.com/songgao/water"
	"golang.org/x/net/ipv4"
)

var (
	configFile = flag.String("config", "", "Path to client config file (recommended)")
	dohServer  = flag.String("doh", "https://dns.yandex.ru/dns-query", "DoH server URL")
	domain     = flag.String("domain", "vpn.example.com", "Base domain for VPN")
	key        = flag.String("key", "", "Hex-encoded encryption key (64 chars)")
	secret     = flag.String("secret", "", "Hex-encoded HMAC secret (32+ chars)")
	style      = flag.String("style", "mixed", "Subdomain style: api, cdn, storage, mixed")
)

const (
	BUFFER_SIZE = 1500
	MTU         = "1300"
	LOCAL_IP    = "10.0.0.1/24"
)

func main() {
	flag.Parse()

	var cryptoKey, hmacSecret []byte
	var err error

	// Загружаем конфигурацию
	if *configFile != "" {
		// Загружаем из конфиг файла
		cfg, err := config.LoadClientConfig(*configFile)
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}

		*domain = cfg.Domain
		*dohServer = cfg.DoHServer
		*style = cfg.SubdomainStyle

		cryptoKey, err = hex.DecodeString(cfg.EncryptionKey)
		if err != nil {
			log.Fatalf("Invalid encryption key in config: %v", err)
		}

		hmacSecret, err = hex.DecodeString(cfg.HMACSecret)
		if err != nil {
			log.Fatalf("Invalid HMAC secret in config: %v", err)
		}

		log.Printf("Loaded configuration from: %s", *configFile)
		log.Printf("Domain: %s", *domain)
		log.Printf("DoH Server: %s", *dohServer)
	} else {
		// Используем флаги командной строки
		if *key == "" || *secret == "" {
			log.Fatalln("Encryption key and HMAC secret are required (use --config or --key/--secret)")
		}

		cryptoKey, err = hex.DecodeString(*key)
		if err != nil {
			log.Fatalf("Invalid key format: %v", err)
		}

		hmacSecret, err = hex.DecodeString(*secret)
		if err != nil {
			log.Fatalf("Invalid secret format: %v", err)
		}
	}

	// Инициализируем crypto и validator
	cipher, err := crypto.NewCipher(cryptoKey)
	if err != nil {
		log.Fatalf("Failed to create cipher: %v", err)
	}

	validator := encoding.NewValidator(hmacSecret, 8)

	// Создаём DoH клиент
	dohClient := doh.NewClient(*dohServer, 10*time.Second)

	// Генерируем session ID
	sessionID := generateSessionID()
	log.Printf("Session ID: %s", sessionID)

	// Создаём TUN интерфейс
	config := water.Config{
		DeviceType: water.TUN,
	}
	config.Name = "zen-tun"

	iface, err := water.New(config)
	if err != nil {
		log.Fatalln("Failed to create TUN interface:", err)
	}

	log.Printf("TUN interface created: %s", iface.Name())

	// Настраиваем интерфейс
	utils.RunIP("link", "set", "dev", iface.Name(), "mtu", MTU)
	utils.RunIP("addr", "add", LOCAL_IP, "dev", iface.Name())
	utils.RunIP("link", "set", "dev", iface.Name(), "up")

	// ВАЖНО: Добавляем route exceptions для DoH сервера ПЕРЕД настройкой VPN маршрутов
	// Cloudflare DNS: 104.16.0.0/13 covers cloudflare-dns.com IPs
	// Google DNS: 8.8.8.0/24, 8.8.4.0/24
	log.Printf("Adding route exceptions for DoH servers...")
	addRouteException("104.16.0.0/13")  // Cloudflare
	addRouteException("1.1.1.0/24")     // Cloudflare DNS
	addRouteException("1.0.0.0/24")     // Cloudflare DNS
	addRouteException("8.8.8.0/24")     // Google DNS
	addRouteException("8.8.4.0/24")     // Google DNS
	addRouteException("77.88.8.0/24")   // Yandex DNS

	// Теперь настраиваем VPN маршруты (они перехватывают весь остальной трафик)
	utils.RunIP("route", "add", "0.0.0.0/1", "dev", iface.Name())
	utils.RunIP("route", "add", "128.0.0.0/1", "dev", iface.Name())

	// Запускаем downstream polling (получение пакетов от сервера)
	go pollDownstream(dohClient, *domain, sessionID, cipher, iface)

	// Основной цикл: читаем пакеты из TUN и отправляем через DoH
	sequence := 0
	packet := make([]byte, BUFFER_SIZE)

	for {
		n, err := iface.Read(packet)
		if err != nil {
			log.Printf("TUN read error: %v", err)
			continue
		}

		if n == 0 {
			continue
		}

		// Парсим IP заголовок для логирования
		if header, err := ipv4.ParseHeader(packet[:n]); err == nil {
			log.Printf("Upstream packet: %s -> %s, size=%d", header.Src, header.Dst, n)
		}

		// Шифруем пакет
		encrypted, err := cipher.Encrypt(packet[:n])
		if err != nil {
			log.Printf("Encryption failed: %v", err)
			continue
		}

		// Подписываем HMAC
		signed := validator.SignData(encrypted)

		// Разбиваем на chunks
		maxChunkSize := encoding.MaxChunkSize()
		chunks := encoding.ChunkData(signed, maxChunkSize)
		totalChunks := len(chunks)

		log.Printf("Packet split into %d chunks", totalChunks)

		// Отправляем все chunks с одним session ID
		chunksSuccessful := 0
		for chunkIdx, chunk := range chunks {
			// Добавляем заголовок: 1 byte = total chunks, 1 byte = chunk index
			header := []byte{byte(totalChunks), byte(chunkIdx)}
			chunkWithHeader := append(header, chunk...)

			// Кодируем chunk в subdomain
			encodedData, err := encoding.EncodeToSubdomain(chunkWithHeader, *style)
			if err != nil {
				log.Printf("Encoding chunk %d failed: %v", chunkIdx, err)
				break
			}

			// Создаём DNS query name
			queryName := encoding.MakeQueryName(sessionID, sequence, encodedData, *domain)

			// Отправляем через DoH
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, err = dohClient.QueryA(ctx, queryName)
			cancel()

			if err != nil {
				log.Printf("DoH query for chunk %d failed: %v", chunkIdx, err)
				break
			}

			chunksSuccessful++
			sequence++
		}

		if chunksSuccessful == totalChunks {
			log.Printf("Successfully sent all %d chunks", totalChunks)
		} else {
			log.Printf("Failed to send complete packet: %d/%d chunks sent", chunksSuccessful, totalChunks)
		}
	}
}

// pollDownstream опрашивает сервер для получения downstream пакетов
func pollDownstream(client *doh.Client, domain, sessionID string, cipher *crypto.Cipher, iface *water.Interface) {
	pollID := 0
	emptyCount := 0

	for {
		// Формируем TXT query для polling
		queryName := fmt.Sprintf("resp-%s-%d.%s", sessionID, pollID, domain)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		resp, err := client.QueryTXT(ctx, queryName)
		cancel()

		if err != nil {
			log.Printf("Downstream poll error: %v", err)
			time.Sleep(1 * time.Second)
			continue
		}

		// Извлекаем TXT данные
		txtRecords, err := doh.ExtractTXTData(resp)
		if err != nil || len(txtRecords) == 0 || txtRecords[0] == "" {
			// Нет данных, увеличиваем интервал polling
			emptyCount++
			if emptyCount > 5 {
				time.Sleep(500 * time.Millisecond)
			} else {
				time.Sleep(100 * time.Millisecond)
			}
			pollID++
			continue
		}

		emptyCount = 0

		// Декодируем hex данные
		hexData := ""
		for _, txt := range txtRecords {
			hexData += txt
		}

		encrypted, err := hex.DecodeString(hexData)
		if err != nil {
			log.Printf("Failed to decode TXT data: %v", err)
			pollID++
			continue
		}

		// Расшифровываем
		packet, err := cipher.Decrypt(encrypted)
		if err != nil {
			log.Printf("Failed to decrypt downstream packet: %v", err)
			pollID++
			continue
		}

		// Парсим IP заголовок
		if header, err := ipv4.ParseHeader(packet); err == nil {
			log.Printf("Downstream packet: %s -> %s, size=%d", header.Src, header.Dst, len(packet))
		}

		// Записываем в TUN
		if _, err := iface.Write(packet); err != nil {
			log.Printf("Failed to write to TUN: %v", err)
		}

		pollID++
	}
}

// generateSessionID генерирует случайный session ID
func generateSessionID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// addRouteException добавляет исключение в маршрутизации для DoH сервера
// чтобы DoH трафик не уходил через VPN туннель
func addRouteException(network string) {
	// Получаем default gateway и interface
	gateway, iface := getDefaultRoute()
	if gateway == "" || iface == "" {
		log.Printf("Warning: Could not detect default route, skipping exception for %s", network)
		return
	}

	// Добавляем route exception
	utils.RunIP("route", "add", network, "via", gateway, "dev", iface)
	log.Printf("Added route exception: %s via %s dev %s", network, gateway, iface)
}

// getDefaultRoute возвращает default gateway и interface
func getDefaultRoute() (gateway, iface string) {
	// Используем ip route show default
	output := utils.RunCommandOutput("ip", "route", "show", "default")

	// Парсим вывод: "default via 192.168.1.1 dev wlp1s0 ..."
	parts := strings.Fields(output)
	for i, part := range parts {
		if part == "via" && i+1 < len(parts) {
			gateway = parts[i+1]
		}
		if part == "dev" && i+1 < len(parts) {
			iface = parts[i+1]
		}
	}

	return gateway, iface
}
