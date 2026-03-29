package doh

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/miekg/dns"
)

// Client для отправки DoH запросов
type Client struct {
	serverURL  string        // DoH endpoint (например, https://dns.yandex.ru/dns-query)
	httpClient *http.Client
	timeout    time.Duration
}

// NewClient создаёт DoH клиент для указанного сервера
func NewClient(serverURL string, timeout time.Duration) *Client {
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	return &Client{
		serverURL: serverURL,
		httpClient: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		timeout: timeout,
	}
}

// QueryA отправляет A запрос (IPv4) через DoH
func (c *Client) QueryA(ctx context.Context, domain string) (*dns.Msg, error) {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(domain), dns.TypeA)
	msg.RecursionDesired = true
	return c.query(ctx, msg)
}

// QueryAAAA отправляет AAAA запрос (IPv6) через DoH
func (c *Client) QueryAAAA(ctx context.Context, domain string) (*dns.Msg, error) {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(domain), dns.TypeAAAA)
	msg.RecursionDesired = true
	return c.query(ctx, msg)
}

// QueryTXT отправляет TXT запрос через DoH
func (c *Client) QueryTXT(ctx context.Context, domain string) (*dns.Msg, error) {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(domain), dns.TypeTXT)
	msg.RecursionDesired = true
	return c.query(ctx, msg)
}

// query отправляет DNS запрос через DoH (RFC 8484)
func (c *Client) query(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
	// Упаковываем DNS message в wire format
	packed, err := msg.Pack()
	if err != nil {
		return nil, fmt.Errorf("failed to pack DNS message: %w", err)
	}

	// Используем POST метод с application/dns-message
	// (более надёжный чем GET для больших запросов)
	req, err := http.NewRequestWithContext(ctx, "POST", c.serverURL, bytes.NewReader(packed))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	// Отправляем запрос
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send DoH request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DoH server returned status %d", resp.StatusCode)
	}

	// Читаем ответ
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Распаковываем DNS response
	response := new(dns.Msg)
	if err := response.Unpack(body); err != nil {
		return nil, fmt.Errorf("failed to unpack DNS response: %w", err)
	}

	return response, nil
}

// QueryGET отправляет DoH запрос через GET (альтернативный метод)
// Используется для совместимости с некоторыми серверами
func (c *Client) QueryGET(ctx context.Context, msg *dns.Msg) (*dns.Msg, error) {
	packed, err := msg.Pack()
	if err != nil {
		return nil, fmt.Errorf("failed to pack DNS message: %w", err)
	}

	// Кодируем в base64url для GET параметра
	encoded := base64.RawURLEncoding.EncodeToString(packed)

	url := fmt.Sprintf("%s?dns=%s", c.serverURL, encoded)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/dns-message")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send DoH request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DoH server returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	response := new(dns.Msg)
	if err := response.Unpack(body); err != nil {
		return nil, fmt.Errorf("failed to unpack DNS response: %w", err)
	}

	return response, nil
}

// ExtractTXTData извлекает данные из TXT records
func ExtractTXTData(msg *dns.Msg) ([]string, error) {
	if msg == nil || len(msg.Answer) == 0 {
		return nil, fmt.Errorf("no answers in DNS response")
	}

	var txtRecords []string
	for _, answer := range msg.Answer {
		if txt, ok := answer.(*dns.TXT); ok {
			txtRecords = append(txtRecords, txt.Txt...)
		}
	}

	if len(txtRecords) == 0 {
		return nil, fmt.Errorf("no TXT records found")
	}

	return txtRecords, nil
}
