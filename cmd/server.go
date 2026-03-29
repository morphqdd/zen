package main

import (
	"encoding/hex"
	"flag"
	"log"

	"zen/internal/config"
	"zen/internal/crypto"
	"zen/internal/dnsserver"
	"zen/internal/encoding"
	"zen/internal/session"
	"zen/internal/utils"

	"github.com/songgao/water"
	"golang.org/x/net/ipv4"
)

var (
	configFile = flag.String("config", "", "Path to server config file (recommended)")
	domain     = flag.String("domain", "vpn.example.com", "Base domain for VPN")
	listenAddr = flag.String("listen", ":5353", "DNS server listen address")
	key        = flag.String("key", "", "Hex-encoded encryption key (64 chars)")
	secret     = flag.String("secret", "", "Hex-encoded HMAC secret (32+ chars)")
)

const (
	BUFFER_SIZE = 1500
	MTU         = "1300"
	SERVER_IP   = "10.0.0.254/24"
)

func main() {
	flag.Parse()

	var cryptoKey, hmacSecret []byte
	var err error

	// Загружаем конфигурацию
	if *configFile != "" {
		// Загружаем из конфиг файла
		cfg, err := config.LoadServerConfig(*configFile)
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}

		*domain = cfg.Domain
		*listenAddr = cfg.ListenAddr

		cryptoKey, err = hex.DecodeString(cfg.EncryptionKey)
		if err != nil {
			log.Fatalf("Invalid encryption key in config: %v", err)
		}

		hmacSecret, err = hex.DecodeString(cfg.HMACSecret)
		if err != nil {
			log.Fatalf("Invalid HMAC secret in config: %v", err)
		}

		log.Printf("Loaded configuration from: %s", *configFile)
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

	// Инициализируем компоненты
	cipher, err := crypto.NewCipher(cryptoKey)
	if err != nil {
		log.Fatalf("Failed to create cipher: %v", err)
	}

	validator := encoding.NewValidator(hmacSecret, 8)
	sessions := session.NewManager(60)

	// Создаём TUN интерфейс
	config := water.Config{
		DeviceType: water.TUN,
	}
	config.Name = "zen-srv"

	iface, err := water.New(config)
	if err != nil {
		log.Fatalln("Failed to create TUN interface:", err)
	}

	log.Printf("TUN interface created: %s", iface.Name())

	// Настраиваем интерфейс
	utils.RunIP("link", "set", "dev", iface.Name(), "mtu", MTU)
	utils.RunIP("addr", "add", SERVER_IP, "dev", iface.Name())
	utils.RunIP("link", "set", "dev", iface.Name(), "up")

	// Включаем IP forwarding
	log.Println("Enabling IP forwarding...")
	utils.RunCommand("sysctl", "-w", "net.ipv4.ip_forward=1")

	// Настраиваем NAT (для выхода в интернет)
	log.Println("Setting up NAT...")
	utils.RunCommand("iptables", "-t", "nat", "-A", "POSTROUTING", "-s", "10.0.0.0/24", "-j", "MASQUERADE")
	utils.RunCommand("iptables", "-A", "FORWARD", "-i", iface.Name(), "-j", "ACCEPT")
	utils.RunCommand("iptables", "-A", "FORWARD", "-o", iface.Name(), "-j", "ACCEPT")

	// Создаём канал для передачи пакетов между DNS сервером и TUN
	upstreamQueue := make(chan []byte, 100)

	// Создаём и запускаем DNS сервер
	dnsServer := dnsserver.NewServer(*domain, *listenAddr, validator, cipher, sessions)
	dnsServer.SetPacketQueue(upstreamQueue)

	if err := dnsServer.Start(); err != nil {
		log.Fatalf("Failed to start DNS server: %v", err)
	}

	// Горутина для обработки upstream пакетов (DNS -> TUN -> Internet)
	go func() {
		for packet := range upstreamQueue {
			if header, err := ipv4.ParseHeader(packet); err == nil {
				log.Printf("Writing to TUN: %s -> %s, size=%d", header.Src, header.Dst, len(packet))
			}

			if _, err := iface.Write(packet); err != nil {
				log.Printf("Failed to write to TUN: %v", err)
			}
		}
	}()

	// Основной цикл: читаем ответные пакеты из TUN и добавляем в сессии
	log.Println("Server started, waiting for packets...")
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

		// Парсим IP заголовок чтобы определить куда отправить пакет
		header, err := ipv4.ParseHeader(packet[:n])
		if err != nil {
			log.Printf("Failed to parse IP header: %v", err)
			continue
		}

		log.Printf("Downstream packet: %s -> %s, size=%d", header.Src, header.Dst, n)

		// Определяем session по destination IP (клиенту)
		// В реальности нужна более сложная логика маппинга IP -> session
		// Для упрощения добавляем во все активные сессии
		activeCount := sessions.Count()
		if activeCount == 0 {
			log.Printf("No active sessions, dropping packet")
			continue
		}

		// TODO: улучшить маппинг IP -> session
		// Сейчас просто добавляем в первую найденную сессию
		// В production нужен connection tracking (NAT state table)
		allSessions := sessions.GetAllSessions()
		if len(allSessions) > 0 {
			// Пока что добавляем в первую сессию
			allSessions[0].EnqueueDownstream(packet[:n])
			log.Printf("Packet queued for downstream delivery")
		}
	}
}
