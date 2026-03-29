# Zen VPN - Quick Start Guide

## Предварительные требования

1. **Сервер** (VPS):
   - Linux (Ubuntu 22.04+, Debian 11+)
   - Root доступ
   - Публичный IP

2. **Домен**:
   - Любой домен (example.com)
   - Доступ к DNS записям

3. **Клиент**:
   - Linux с root/sudo
   - Go 1.21+ (для сборки)

## Шаг 1: Подготовка сервера

```bash
# На сервере
cd /root
git clone https://github.com/yourusername/zen
cd zen

# Собираем
make build

# Инициализируем (генерирует ключи автоматически)
sudo ./zenctl init --domain vpn.example.com

# ✓ Server initialized successfully!
# Configuration saved to: /etc/zen/server.conf
```

## Шаг 2: Настройка DNS

Добавьте NS записи для вашего домена:

```dns
# У вашего DNS провайдера (Cloudflare, AWS Route53, etc):

vpn.example.com.    IN  NS  ns1.example.com.
ns1.example.com.    IN  A   <YOUR_SERVER_IP>

# Или напрямую:
vpn.example.com.    IN  NS  <YOUR_SERVER_IP>
```

**Проверка DNS:**
```bash
# Подождите 5-10 минут для propagation, затем:
dig @<YOUR_SERVER_IP> test.vpn.example.com

# Должно быть что-то типа:
# ;; ANSWER SECTION:
# test.vpn.example.com. 300 IN A 104.16.0.1
```

## Шаг 3: Запуск сервера

```bash
# Запуск в foreground (для тестирования)
sudo ./zen-server --config /etc/zen/server.conf

# Вывод:
# TUN interface created: zen-srv
# Enabling IP forwarding...
# Setting up NAT...
# Starting DNS server (UDP) on :53 for domain vpn.example.com
# Server started, waiting for packets...
```

**Для production (systemd service):**
```bash
sudo tee /etc/systemd/system/zen-server.service > /dev/null <<EOF
[Unit]
Description=Zen VPN Server
After=network.target

[Service]
Type=simple
User=root
ExecStart=/usr/local/bin/zen-server --config /etc/zen/server.conf
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable zen-server
sudo systemctl start zen-server
sudo systemctl status zen-server
```

## Шаг 4: Создание клиента

```bash
# На сервере
sudo ./zenctl add-client alice

# ✓ Client 'alice' added successfully!
# Configuration saved to: /etc/zen/clients/alice.json

# Показать конфигурацию
sudo ./zenctl show-client alice

# Скопируйте вывод или JSON файл
```

## Шаг 5: Запуск клиента

```bash
# На клиентской машине
cd /home/user/zen
make build

# Если у вас есть config file:
sudo ./zen-client --config /tmp/alice.json

# Или через параметры командной строки:
sudo ./zen-client \
  --domain vpn.example.com \
  --doh https://dns.yandex.ru/dns-query \
  --key <ENCRYPTION_KEY> \
  --secret <HMAC_SECRET> \
  --style mixed

# Вывод:
# TUN interface created: zen-tun
# Session ID: a1b2c3d4e5f6
# Loaded configuration from: /tmp/alice.json
# Domain: vpn.example.com
# DoH Server: https://dns.yandex.ru/dns-query
```

## Шаг 6: Тестирование

### На клиенте:

```bash
# Проверка IP (должен показать IP сервера)
curl ifconfig.me

# Проверка DNS
dig google.com

# Проверка latency
ping -c 5 8.8.8.8

# Speed test
curl -o /dev/null https://speed.cloudflare.com/__down?bytes=10000000
```

### На сервере (мониторинг):

```bash
# Логи
sudo journalctl -u zen-server -f

# Статистика
sudo ip -s link show zen-srv

# Сетевой трафик
sudo tcpdump -i zen-srv -n

# Активные сессии
sudo ./zenctl list-clients
```

## Troubleshooting

### Клиент не подключается

```bash
# 1. Проверьте DNS через DoH
curl -H "accept: application/dns-json" \
  "https://dns.yandex.ru/dns-query?name=test.vpn.example.com"

# 2. Проверьте что сервер запущен
sudo systemctl status zen-server

# 3. Проверьте firewall
sudo ufw allow 53/udp
sudo ufw allow 53/tcp

# 4. Проверьте логи
sudo journalctl -u zen-server --since "5 minutes ago"
```

### Высокая латентность

```bash
# Попробуйте другой DoH провайдер
--doh https://dns.google/dns-query
--doh https://cloudflare-dns.com/dns-query

# Проверьте latency до DoH сервера
ping dns.yandex.ru
```

### Пакеты не проходят

```bash
# На сервере:
# Проверьте IP forwarding
sysctl net.ipv4.ip_forward
# Должно быть: net.ipv4.ip_forward = 1

# Проверьте NAT
sudo iptables -t nat -L -n -v

# Если нет MASQUERADE:
sudo iptables -t nat -A POSTROUTING -s 10.0.0.0/24 -j MASQUERADE
```

## Performance tuning

### MTU optimization:
```bash
# Если есть packet loss, уменьшите MTU
# В /etc/zen/server.conf:
"mtu": "1200"  # вместо 1300
```

### Polling rate:
```go
// В internal/doh/client.go измените timeout
timeout: 5 * time.Second  // увеличьте для slower networks
```

## Безопасность

### Ротация ключей:
```bash
# 1. Создайте новую конфигурацию
sudo ./zenctl init --domain vpn.example.com --config-dir /etc/zen-new

# 2. Мигрируйте клиентов постепенно
sudo ./zenctl --config-dir /etc/zen-new add-client alice

# 3. После миграции всех клиентов
sudo mv /etc/zen /etc/zen-old
sudo mv /etc/zen-new /etc/zen
sudo systemctl restart zen-server
```

### Мониторинг атак:
```bash
# Проверяйте количество invalid HMAC
sudo journalctl -u zen-server | grep "HMAC validation failed"

# Если много - возможно bruteforce, смените ключи
```

## Next steps

- Настройте автоматический старт клиента (systemd)
- Добавьте split tunneling (только определённые сайты через VPN)
- Настройте fail2ban для защиты сервера
- Добавьте мониторинг (Prometheus + Grafana)
