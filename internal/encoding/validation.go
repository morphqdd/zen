package encoding

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"time"
)

// Validator проверяет подлинность VPN запросов через HMAC
type Validator struct {
	secret []byte
	hmacLen int // длина HMAC в hex символах (обычно 8-12)
}

// NewValidator создаёт validator с секретным ключом
func NewValidator(secret []byte, hmacLen int) *Validator {
	if hmacLen < 6 {
		hmacLen = 8 // минимум 8 hex chars (4 bytes)
	}
	if hmacLen > 16 {
		hmacLen = 16 // максимум 16 hex chars (8 bytes)
	}
	return &Validator{
		secret: secret,
		hmacLen: hmacLen,
	}
}

// SignData добавляет HMAC подпись к данным
// Возвращает: data + timestamp + hmac
func (v *Validator) SignData(data []byte) []byte {
	// Добавляем timestamp (4 bytes, unix time / 60 для минутной точности)
	timestamp := uint32(time.Now().Unix() / 60)
	timestampBytes := []byte{
		byte(timestamp >> 24),
		byte(timestamp >> 16),
		byte(timestamp >> 8),
		byte(timestamp),
	}

	// Вычисляем HMAC от data + timestamp
	h := hmac.New(sha256.New, v.secret)
	h.Write(data)
	h.Write(timestampBytes)
	fullHmac := h.Sum(nil)

	// Берём только первые N байт HMAC
	hmacBytes := fullHmac[:v.hmacLen/2]

	// Собираем: data + timestamp + hmac
	signed := make([]byte, len(data)+len(timestampBytes)+len(hmacBytes))
	copy(signed, data)
	copy(signed[len(data):], timestampBytes)
	copy(signed[len(data)+len(timestampBytes):], hmacBytes)

	return signed
}

// VerifyData проверяет HMAC подпись и возвращает оригинальные данные
// maxAge - максимальный возраст запроса в секундах (0 = не проверять)
func (v *Validator) VerifyData(signed []byte, maxAge int) ([]byte, error) {
	hmacSize := v.hmacLen / 2
	timestampSize := 4
	minSize := timestampSize + hmacSize

	if len(signed) < minSize {
		return nil, fmt.Errorf("signed data too short")
	}

	// Извлекаем компоненты
	dataLen := len(signed) - timestampSize - hmacSize
	data := signed[:dataLen]
	timestampBytes := signed[dataLen : dataLen+timestampSize]
	receivedHmac := signed[dataLen+timestampSize:]

	// Проверяем timestamp если требуется
	if maxAge > 0 {
		timestamp := uint32(timestampBytes[0])<<24 |
			uint32(timestampBytes[1])<<16 |
			uint32(timestampBytes[2])<<8 |
			uint32(timestampBytes[3])

		currentTime := uint32(time.Now().Unix() / 60)
		age := int(currentTime - timestamp)

		if age > maxAge/60 || age < -5 { // разрешаем 5 минут в будущее (для clock skew)
			return nil, fmt.Errorf("timestamp expired or invalid")
		}
	}

	// Вычисляем ожидаемый HMAC
	h := hmac.New(sha256.New, v.secret)
	h.Write(data)
	h.Write(timestampBytes)
	expectedHmac := h.Sum(nil)[:hmacSize]

	// Сравниваем HMAC (constant-time для защиты от timing attacks)
	if !hmac.Equal(receivedHmac, expectedHmac) {
		return nil, fmt.Errorf("invalid HMAC signature")
	}

	return data, nil
}

// EncodeSecureSubdomain кодирует данные с HMAC подписью в subdomain
func (v *Validator) EncodeSecureSubdomain(data []byte, style string) (string, error) {
	// Подписываем данные
	signed := v.SignData(data)

	// Кодируем в subdomain
	return EncodeToSubdomain(signed, style)
}

// DecodeSecureSubdomain декодирует и проверяет subdomain
// maxAge - максимальный возраст в секундах (300 = 5 минут)
func (v *Validator) DecodeSecureSubdomain(subdomain string, maxAge int) ([]byte, error) {
	// Декодируем subdomain
	signed, err := DecodeFromSubdomain(subdomain)
	if err != nil {
		return nil, err
	}

	// Проверяем подпись и извлекаем данные
	data, err := v.VerifyData(signed, maxAge)
	if err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	return data, nil
}

// GenerateFakeResponse генерирует реалистичный fake IP для маскировки
// При невалидных запросах возвращаем IP известных сервисов
func GenerateFakeResponse(subdomain string) string {
	// Хешируем subdomain для детерминированного выбора
	h := sha256.Sum256([]byte(subdomain))
	index := int(h[0]) % len(fakeIPs)
	return fakeIPs[index]
}

// Список реалистичных IP адресов для fake responses
// IP известных CDN, cloud providers, API endpoints
var fakeIPs = []string{
	// Cloudflare CDN
	"104.16.0.1", "104.17.0.1", "104.18.0.1",
	// Fastly CDN
	"151.101.1.1", "151.101.65.1",
	// AWS CloudFront
	"13.224.0.1", "13.249.0.1",
	// Google Cloud CDN
	"34.107.0.1", "35.186.0.1",
	// Akamai
	"23.0.0.1", "104.64.0.1",
	// Yandex
	"77.88.55.60", "77.88.55.80",
}
