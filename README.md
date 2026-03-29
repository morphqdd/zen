# Zen - DoH-based VPN

DoH (DNS over HTTPS) VPN с маскировкой под легитимные API запросы и HMAC аутентификацией.

## Особенности

- ✅ **DNS over HTTPS туннелирование** через публичные резолверы (Yandex, Google, Cloudflare)
- ✅ **Маскировка под легитимные API** - запросы выглядят как обращения к CDN/API/Storage
- ✅ **Шифрование XChaCha20-Poly1305** - современное AEAD шифрование
- ✅ **HMAC аутентификация** - защита от поддельных запросов и replay attacks
- ✅ **Stealth mode** - невалидные запросы получают fake IP ответы
- ✅ **Простое управление** - генерация конфигов как в WireGuard

## Архитектура

```
Client → Yandex DoH → Ваш Authoritative DNS → Internet
         (dns.yandex.ru)    (vpn.example.com)
```

**Upstream (Client → Server):**
```
IP packet → Encrypt (ChaCha20) → Sign (HMAC) →
→ Encode to subdomain (api-users-auth-3f4a.vpn.example.com) →
→ DNS A/AAAA query via DoH → Server
```

**Downstream (Server → Client):**
```
Server → Encrypt → Store in session →
← Client polls (TXT query: resp-sessionid-123.vpn.example.com) ←
← TXT record with encrypted data
```

## Установка

```bash
# Клонируем репозиторий
git clone https://github.com/yourusername/zen
cd zen

# Собираем бинарники
make build

# Бинарники:
# - zenctl      - утилита управления конфигурацией
# - zen-server  - VPN сервер
# - zen-client  - VPN клиент
```

## Быстрый старт

### 1. Настройка сервера

```bash
# Инициализируем сервер (генерирует ключи автоматически)
sudo ./zenctl init --domain vpn.example.com

# ✓ Server initialized successfully!
# Configuration saved to: /etc/zen/server.conf
```

### 2. Настройка DNS

Добавьте NS записи для вашего домена:

```dns
vpn.example.com.  IN  NS  ns1.example.com.
ns1.example.com.  IN  A   <YOUR_SERVER_IP>
```

Проверьте что DNS работает:

```bash
dig @<YOUR_SERVER_IP> test.vpn.example.com
```

### 3. Запуск сервера

```bash
# Запускаем сервер с конфигом
sudo ./zen-server --config /etc/zen/server.conf

# TUN interface created: zen-srv
# Starting DNS server (UDP) on :53 for domain vpn.example.com
# Server started, waiting for packets...
```

### 4. Добавление клиентов

```bash
# Создаём клиента
sudo ./zenctl add-client alice

# ✓ Client 'alice' added successfully!
# Configuration saved to: /etc/zen/clients/alice.json

# Показываем конфигурацию для клиента
sudo ./zenctl show-client alice

# Configuration for client: alice
#
# domain = "vpn.example.com"
# doh_server = "https://dns.yandex.ru/dns-query"
# encryption_key = "..."
# hmac_secret = "..."
# ...
```

### 5. Подключение клиента

```bash
# Копируем конфиг на клиентскую машину
scp /etc/zen/clients/alice.json client:/tmp/zen-alice.json

# На клиенте:
sudo ./zen-client --config /tmp/zen-alice.json

# TUN interface created: zen-tun
# Session ID: a1b2c3d4e5f6
# Loaded configuration from: /tmp/zen-alice.json
```

Готово! Теперь весь трафик клиента идёт через VPN.

## Команды zenctl

```bash
# Инициализация сервера
zenctl init --domain vpn.example.com [--listen :53]

# Управление клиентами
zenctl add-client <name> [--doh URL] [--style api|cdn|mixed]
zenctl list-clients
zenctl show-client <name> [--format text|json]
zenctl remove-client <name>

# Версия
zenctl version
```

## Стили маскировки subdomain

### API Style (`--style api`)
```
api-users-auth-3f4a2b1c.vpn.example.com
api-v2-posts-list-5c6d7e8f.vpn.example.com
api-v3-token-refresh-a1b2c3d4.vpn.example.com
```

### CDN Style (`--style cdn`)
```
cdn-images-thumb-9e8f7a6b.vpn.example.com
static-css-compressed-1a2b3c4d.vpn.example.com
media-videos-preview-5e6f7g8h.vpn.example.com
```

### Storage Style (`--style storage`)
```
s3-bucket-upload-7h8i9j0k.vpn.example.com
storage-file-get-2c3d4e5f.vpn.example.com
cache-temp-sync-6g7h8i9j.vpn.example.com
```

### Mixed Style (`--style mixed`) - Рекомендуется
Случайная комбинация всех стилей для максимальной маскировки.

## Альтернативные DoH провайдеры

```bash
# При добавлении клиента можно указать DoH сервер:

# Google DoH
zenctl add-client bob --doh https://dns.google/dns-query

# Cloudflare DoH
zenctl add-client charlie --doh https://cloudflare-dns.com/dns-query

# AdGuard DNS
zenctl add-client dave --doh https://dns.adguard.com/dns-query
```

## Безопасность

### Шифрование
- **XChaCha20-Poly1305** - AEAD шифрование с 256-битным ключом
- Каждый пакет имеет уникальный nonce
- Authenticated encryption предотвращает tampering

### Аутентификация
- **HMAC-SHA256** подпись всех запросов
- Timestamp защита (TTL 5 минут)
- Replay attack protection

### Stealth Mode
- Невалидные HMAC → возвращается fake IP (CDN/Cloud providers)
- Выглядит как обычный DNS сервер для сканеров
- Нет индикаторов VPN туннеля

## Производительность

| Параметр | Значение |
|----------|----------|
| MTU | 1300 bytes |
| Latency | 100-300ms (зависит от DoH) |
| Throughput | 100-500 KB/s |
| DNS Overhead | ~40-60% |

### Оптимизация

1. **Используйте ближайший DoH сервер**
   ```bash
   # Для России
   --doh https://dns.yandex.ru/dns-query

   # Для Европы
   --doh https://cloudflare-dns.com/dns-query
   ```

2. **Настройте connection pooling**
   - DoH клиент уже использует HTTP/2 multiplexing
   - Keep-alive connections снижают latency

3. **Локальный DoH resolver**
   - Поднимите свой DoH-to-DNS proxy рядом с клиентом
   - Снижает latency на 50-100ms

## Troubleshooting

### Клиент не подключается

```bash
# 1. Проверьте DNS через сам сервер
dig @<SERVER_IP> test.vpn.example.com

# 2. Проверьте через Yandex DoH
curl -H "accept: application/dns-json" \
  "https://dns.yandex.ru/dns-query?name=test.vpn.example.com"

# 3. Проверьте что NS записи правильные
dig NS vpn.example.com
```

### Пакеты не проходят

```bash
# На сервере:

# Проверьте IP forwarding
sysctl net.ipv4.ip_forward
# Должно быть: net.ipv4.ip_forward = 1

# Проверьте NAT правила
iptables -t nat -L -n -v

# Проверьте TUN интерфейс
ip addr show zen-srv
ip route show
```

### Высокий latency

```bash
# Проверьте latency до DoH сервера
curl -w "@curl-format.txt" -o /dev/null -s https://dns.yandex.ru/dns-query

# Попробуйте другой DoH сервер
zenctl add-client test --doh https://dns.google/dns-query
```

### Логи

```bash
# Сервер
journalctl -u zen-server -f

# Клиент (если запущен через systemd)
journalctl -u zen-client -f
```

## Структура конфигурации

### Server Config (`/etc/zen/server.conf`)
```json
{
  "domain": "vpn.example.com",
  "listen_addr": ":53",
  "encryption_key": "...",
  "hmac_secret": "...",
  "tun_name": "zen-srv",
  "server_ip": "10.0.0.254/24",
  "mtu": "1300",
  "next_client_id": 1
}
```

### Client Config (`/etc/zen/clients/alice.json`)
```json
{
  "client_id": 1,
  "client_name": "alice",
  "domain": "vpn.example.com",
  "doh_server": "https://dns.yandex.ru/dns-query",
  "encryption_key": "...",
  "hmac_secret": "...",
  "client_ip": "10.0.0.1/24",
  "subdomain_style": "mixed",
  "mtu": "1300"
}
```

## Дорожная карта

- [ ] Connection tracking для правильного маппинга downstream
- [ ] Автоматический chunking для больших пакетов
- [ ] Adaptive polling rate
- [ ] Metrics и мониторинг (Prometheus)
- [ ] WebSocket fallback для low-latency
- [ ] Mobile clients (iOS/Android)
- [ ] GUI для управления

## Сравнение с другими VPN

| Фича | Zen | WireGuard | OpenVPN | Shadowsocks |
|------|-----|-----------|---------|-------------|
| Stealth | ✅ DNS | ❌ UDP | ⚠️ TLS | ✅ SOCKS5 |
| DPI Evasion | ✅ Excellent | ❌ Poor | ⚠️ Medium | ✅ Good |
| Speed | ⚠️ Medium | ✅ Excellent | ❌ Slow | ✅ Good |
| Setup | ✅ Easy | ✅ Easy | ❌ Complex | ✅ Easy |

## Лицензия

MIT

## Авторы

- morphe - initial implementation

## Благодарности

- Yandex DNS за публичный DoH resolver
- miekg/dns за отличную DNS библиотеку
- songgao/water за TUN/TAP интерфейс
