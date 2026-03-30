package dnsserver

import (
	"encoding/hex"
	"log"
	"net"
	"strings"

	"zen/internal/crypto"
	"zen/internal/encoding"
	"zen/internal/session"

	"github.com/miekg/dns"
)

// Server - authoritative DNS сервер для VPN
type Server struct {
	domain      string              // base domain (например, vpn.example.com)
	listenAddr  string              // адрес для прослушивания (например, :53)
	validator   *encoding.Validator // для проверки HMAC
	cipher      *crypto.Cipher      // для расшифровки данных
	sessions    *session.Manager    // менеджер сессий
	packetQueue chan<- []byte       // очередь для отправки пакетов в интернет
}

// NewServer создаёт новый DNS сервер
func NewServer(domain, listenAddr string, validator *encoding.Validator, cipher *crypto.Cipher, sessions *session.Manager) *Server {
	return &Server{
		domain:     domain,
		listenAddr: listenAddr,
		validator:  validator,
		cipher:     cipher,
		sessions:   sessions,
	}
}

// SetPacketQueue устанавливает канал для отправки декодированных пакетов
func (s *Server) SetPacketQueue(queue chan<- []byte) {
	s.packetQueue = queue
}

// Start запускает DNS сервер
func (s *Server) Start() error {
	dns.HandleFunc(s.domain, s.handleDNSRequest)

	// Запускаем UDP сервер
	udpServer := &dns.Server{
		Addr: s.listenAddr,
		Net:  "udp",
	}

	// Запускаем TCP сервер (для больших запросов)
	tcpServer := &dns.Server{
		Addr: s.listenAddr,
		Net:  "tcp",
	}

	go func() {
		log.Printf("Starting DNS server (UDP) on %s for domain %s", s.listenAddr, s.domain)
		if err := udpServer.ListenAndServe(); err != nil {
			log.Fatalf("Failed to start UDP server: %v", err)
		}
	}()

	go func() {
		log.Printf("Starting DNS server (TCP) on %s for domain %s", s.listenAddr, s.domain)
		if err := tcpServer.ListenAndServe(); err != nil {
			log.Fatalf("Failed to start TCP server: %v", err)
		}
	}()

	return nil
}

// handleDNSRequest обрабатывает DNS запрос
func (s *Server) handleDNSRequest(w dns.ResponseWriter, req *dns.Msg) {
	msg := new(dns.Msg)
	msg.SetReply(req)
	msg.Authoritative = true

	if len(req.Question) == 0 {
		w.WriteMsg(msg)
		return
	}

	question := req.Question[0]
	qname := strings.ToLower(question.Name)
	qtype := question.Qtype

	log.Printf("DNS query: %s type=%s from=%s", qname, dns.TypeToString[qtype], w.RemoteAddr())

	// Обработка в зависимости от типа запроса
	switch qtype {
	case dns.TypeA, dns.TypeAAAA:
		// Upstream: данные от клиента к серверу
		s.handleUpstream(msg, qname)

	case dns.TypeTXT:
		// Downstream: клиент запрашивает данные от сервера
		s.handleDownstream(msg, qname)

	default:
		// Для других типов возвращаем NXDOMAIN
		msg.SetRcode(req, dns.RcodeNameError)
	}

	w.WriteMsg(msg)
}

// handleUpstream обрабатывает upstream запросы (A/AAAA)
// Формат: <session-id>-<seq>.<encoded-data>.<domain>
func (s *Server) handleUpstream(msg *dns.Msg, qname string) {
	// Парсим query name
	sessionID, sequence, chunkData, err := encoding.ParseQueryName(qname, s.domain)
	if err != nil {
		log.Printf("Failed to parse query name: %v", err)
		s.returnFakeIP(msg, qname)
		return
	}

	// Извлекаем заголовок chunk: 2 bytes (total_chunks, chunk_index)
	if len(chunkData) < 2 {
		log.Printf("Chunk data too short for session %s", sessionID)
		s.returnFakeIP(msg, qname)
		return
	}

	totalChunks := int(chunkData[0])
	chunkIndex := int(chunkData[1])
	actualData := chunkData[2:]

	log.Printf("Received chunk %d/%d for session %s (seq=%d, size=%d)",
		chunkIndex+1, totalChunks, sessionID, sequence, len(actualData))

	// Получаем или создаём сессию
	sess := s.sessions.GetOrCreate(sessionID, msg.Question[0].Name)
	sess.AddUpstreamChunk(chunkIndex, actualData)

	// Пытаемся собрать полный пакет
	if signedData, ok := sess.TryAssembleUpstream(totalChunks); ok {
		log.Printf("All chunks received, verifying HMAC...")

		// Проверяем HMAC собранных данных
		encryptedData, err := s.validator.VerifyData(signedData, 0)
		if err != nil {
			log.Printf("HMAC validation failed for session %s: %v", sessionID, err)
			s.returnFakeIP(msg, qname)
			return
		}

		// Расшифровываем данные
		packetData, err := s.cipher.Decrypt(encryptedData)
		if err != nil {
			log.Printf("Decryption failed for session %s: %v", sessionID, err)
			s.returnFakeIP(msg, qname)
			return
		}

		log.Printf("Valid VPN packet: session=%s size=%d", sessionID, len(packetData))

		// Отправляем пакет в очередь для TUN интерфейса
		if s.packetQueue != nil {
			select {
			case s.packetQueue <- packetData:
				log.Printf("Packet queued for internet: %d bytes", len(packetData))
			default:
				log.Printf("Packet queue full, dropping packet")
			}
		}
	}

	// Возвращаем "нормальный" IP в ответе (чтобы не палиться)
	s.returnFakeIP(msg, qname)
}

// handleDownstream обрабатывает downstream запросы (TXT)
// Формат: resp-<session-id>-<poll-id>.<domain>
func (s *Server) handleDownstream(msg *dns.Msg, qname string) {
	// Извлекаем session ID из query
	sessionID := s.extractSessionID(qname)
	if sessionID == "" {
		log.Printf("Invalid downstream query format: %s", qname)
		msg.SetRcode(msg, dns.RcodeNameError)
		return
	}

	// Получаем сессию
	sess, exists := s.sessions.Get(sessionID)
	if !exists {
		log.Printf("Session not found: %s", sessionID)
		msg.SetRcode(msg, dns.RcodeNameError)
		return
	}

	// Получаем следующий пакет из очереди
	packet, hasData := sess.GetDownstreamPacket()
	if !hasData {
		// Нет данных - возвращаем пустой TXT record
		txt := &dns.TXT{
			Hdr: dns.RR_Header{
				Name:   msg.Question[0].Name,
				Rrtype: dns.TypeTXT,
				Class:  dns.ClassINET,
				Ttl:    0, // не кешировать
			},
			Txt: []string{""},
		}
		msg.Answer = append(msg.Answer, txt)
		return
	}

	// Шифруем пакет
	encrypted, err := s.cipher.Encrypt(packet)
	if err != nil {
		log.Printf("Failed to encrypt downstream packet: %v", err)
		msg.SetRcode(msg, dns.RcodeServerFailure)
		return
	}

	// Кодируем в hex для TXT record
	hexData := hex.EncodeToString(encrypted)

	// DNS TXT record может содержать до 255 символов на строку
	// Разбиваем на chunks по 250 символов
	var txtChunks []string
	for i := 0; i < len(hexData); i += 250 {
		end := i + 250
		if end > len(hexData) {
			end = len(hexData)
		}
		txtChunks = append(txtChunks, hexData[i:end])
	}

	txt := &dns.TXT{
		Hdr: dns.RR_Header{
			Name:   msg.Question[0].Name,
			Rrtype: dns.TypeTXT,
			Class:  dns.ClassINET,
			Ttl:    0, // не кешировать
		},
		Txt: txtChunks,
	}

	msg.Answer = append(msg.Answer, txt)
	log.Printf("Returned downstream packet: session=%s size=%d", sessionID, len(packet))
}

// returnFakeIP возвращает реалистичный fake IP в ответе
func (s *Server) returnFakeIP(msg *dns.Msg, qname string) {
	fakeIP := encoding.GenerateFakeResponse(qname)

	var rr dns.RR
	if msg.Question[0].Qtype == dns.TypeA {
		rr = &dns.A{
			Hdr: dns.RR_Header{
				Name:   msg.Question[0].Name,
				Rrtype: dns.TypeA,
				Class:  dns.ClassINET,
				Ttl:    300,
			},
			A: net.ParseIP(fakeIP),
		}
	} else {
		// Для AAAA возвращаем IPv6 адрес
		rr = &dns.AAAA{
			Hdr: dns.RR_Header{
				Name:   msg.Question[0].Name,
				Rrtype: dns.TypeAAAA,
				Class:  dns.ClassINET,
				Ttl:    300,
			},
			AAAA: net.ParseIP("2606:4700::1"),
		}
	}

	msg.Answer = append(msg.Answer, rr)
}

// extractSessionID извлекает session ID из downstream query
// Формат: resp-<session-id>-<poll-id>.<domain>
func (s *Server) extractSessionID(qname string) string {
	// Удаляем домен
	prefix := strings.TrimSuffix(qname, "."+s.domain+".")
	if prefix == qname {
		return ""
	}

	// Извлекаем session ID
	parts := strings.Split(prefix, "-")
	if len(parts) < 3 || parts[0] != "resp" {
		return ""
	}

	// Session ID находится между "resp-" и последним "-<poll-id>"
	return strings.Join(parts[1:len(parts)-1], "-")
}
