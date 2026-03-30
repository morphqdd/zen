package encoding

import (
	"encoding/hex"
	"fmt"
	"math/rand"
	"strings"
	"time"
)

// Словари для генерации реалистичных API-подобных subdomains
var (
	serviceTypes = []string{
		"api", "cdn", "s3", "cache", "edge", "static", "media",
	}

	apiResources = []string{
		"users", "auth", "profile", "session", "token", "posts",
		"comments", "messages", "notifications", "settings", "data",
	}

	apiActions = []string{
		"get", "list", "update", "create", "delete", "refresh",
		"verify", "check", "sync", "fetch", "load",
	}

	cdnResources = []string{
		"images", "videos", "assets", "fonts", "css", "js",
		"files", "docs", "icons", "sprites", "bundles",
	}

	cdnActions = []string{
		"thumb", "preview", "full", "compressed", "optimized",
		"cached", "resized", "cropped", "v1", "v2", "latest",
	}

	storageResources = []string{
		"bucket", "store", "upload", "download", "temp", "archive",
	}

	storageActions = []string{
		"put", "get", "list", "sync", "backup", "restore", "chunk",
	}
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// EncodeToSubdomain кодирует данные в реалистичный API-подобный subdomain
// Примеры:
//   - api-users-auth-3f4a2b1c
//   - cdn-images-thumb-a1b2c3d4
//   - s3-bucket-upload-5c6d7e8f
func EncodeToSubdomain(data []byte, style string) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("empty data")
	}

	// Конвертируем в hex
	hexData := hex.EncodeToString(data)

	// Генерируем реалистичный API path в зависимости от стиля
	var parts []string

	switch style {
	case "api":
		// api-<resource>-<action>-<hex>
		serviceType := "api"
		if rand.Intn(2) == 0 {
			serviceType = fmt.Sprintf("api-v%d", rand.Intn(3)+1) // api-v1, api-v2, api-v3
		}
		resource := apiResources[rand.Intn(len(apiResources))]
		action := apiActions[rand.Intn(len(apiActions))]
		parts = []string{serviceType, resource, action}

	case "cdn":
		// cdn-<resource>-<action>-<hex>
		serviceType := "cdn"
		resource := cdnResources[rand.Intn(len(cdnResources))]
		action := cdnActions[rand.Intn(len(cdnActions))]
		parts = []string{serviceType, resource, action}

	case "storage":
		// s3-<resource>-<action>-<hex>
		serviceType := "s3"
		resource := storageResources[rand.Intn(len(storageResources))]
		action := storageActions[rand.Intn(len(storageActions))]
		parts = []string{serviceType, resource, action}

	default:
		// Random mix для разнообразия
		serviceType := serviceTypes[rand.Intn(len(serviceTypes))]

		switch serviceType {
		case "api":
			resource := apiResources[rand.Intn(len(apiResources))]
			action := apiActions[rand.Intn(len(apiActions))]
			parts = []string{serviceType, resource, action}
		case "cdn", "static", "media":
			resource := cdnResources[rand.Intn(len(cdnResources))]
			action := cdnActions[rand.Intn(len(cdnActions))]
			parts = []string{serviceType, resource, action}
		default: // s3, cache, edge
			resource := storageResources[rand.Intn(len(storageResources))]
			action := storageActions[rand.Intn(len(storageActions))]
			parts = []string{serviceType, resource, action}
		}
	}

	// Ограничиваем hex часть чтобы не превысить DNS limit в 63 символа
	// Оставляем место для semantic parts (~30 chars) + дефисы (~4 chars)
	maxHexLen := 63 - len(strings.Join(parts, "-")) - 1
	if maxHexLen < 8 {
		maxHexLen = 8 // минимум 8 hex символов (4 байта)
	}
	if len(hexData) > maxHexLen {
		hexData = hexData[:maxHexLen]
		// Ensure even length for hex decoding
		if len(hexData)%2 != 0 {
			hexData = hexData[:len(hexData)-1]
		}
	}

	// Собираем: service-resource-action-hexdata
	parts = append(parts, hexData)
	subdomain := strings.Join(parts, "-")

	if len(subdomain) > 63 {
		return "", fmt.Errorf("subdomain too long: %d (max 63)", len(subdomain))
	}

	return subdomain, nil
}

// DecodeFromSubdomain декодирует данные из API-подобного subdomain
// Извлекает hex часть из формата: service-resource-action-hexdata
func DecodeFromSubdomain(subdomain string) ([]byte, error) {
	if subdomain == "" {
		return nil, fmt.Errorf("empty subdomain")
	}

	// Разбиваем по дефисам
	parts := strings.Split(subdomain, "-")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid subdomain format")
	}

	// Последняя часть - это hex данные
	hexData := parts[len(parts)-1]

	// Декодируем из hex
	data, err := hex.DecodeString(hexData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode hex: %w", err)
	}

	return data, nil
}

// ChunkData разбивает большие данные на chunks для отправки в нескольких DNS запросах
// Каждый chunk должен помещаться в один subdomain (max ~200 bytes raw data)
func ChunkData(data []byte, maxChunkSize int) [][]byte {
	if maxChunkSize <= 0 {
		maxChunkSize = 200 // default: ~200 bytes → ~400 hex chars → ~50 chars per chunk with dashes
	}

	var chunks [][]byte
	for i := 0; i < len(data); i += maxChunkSize {
		end := i + maxChunkSize
		if end > len(data) {
			end = len(data)
		}
		chunks = append(chunks, data[i:end])
	}

	return chunks
}

// MakeQueryName создаёт полное DNS query name
// Формат: <session-id>-<seq>.<encoded-data>.<base-domain>
func MakeQueryName(sessionID string, sequence int, encodedData, baseDomain string) string {
	return fmt.Sprintf("%s-%d.%s.%s", sessionID, sequence, encodedData, baseDomain)
}

// ParseQueryName парсит DNS query name и извлекает session ID, sequence и данные
func ParseQueryName(queryName, baseDomain string) (sessionID string, sequence int, data []byte, err error) {
	// Убираем trailing dot если есть
	queryName = strings.TrimSuffix(queryName, ".")
	baseDomain = strings.TrimSuffix(baseDomain, ".")

	// Удаляем base domain
	prefix := strings.TrimSuffix(queryName, "."+baseDomain)
	if prefix == queryName {
		return "", 0, nil, fmt.Errorf("query name doesn't match base domain")
	}

	// Разбиваем на session-seq и encoded-data
	parts := strings.SplitN(prefix, ".", 2)
	if len(parts) != 2 {
		return "", 0, nil, fmt.Errorf("invalid query format")
	}

	// Парсим session-id и sequence
	sessionParts := strings.Split(parts[0], "-")
	if len(sessionParts) < 2 {
		return "", 0, nil, fmt.Errorf("invalid session format")
	}

	sessionID = strings.Join(sessionParts[:len(sessionParts)-1], "-")
	_, err = fmt.Sscanf(sessionParts[len(sessionParts)-1], "%d", &sequence)
	if err != nil {
		return "", 0, nil, fmt.Errorf("invalid sequence: %w", err)
	}

	// Декодируем данные из subdomain
	data, err = DecodeFromSubdomain(parts[1])
	if err != nil {
		return "", 0, nil, fmt.Errorf("failed to decode data: %w", err)
	}

	return sessionID, sequence, data, nil
}
