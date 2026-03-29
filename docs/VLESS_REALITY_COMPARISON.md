# VLESS+Reality Setup Guide (для сравнения с Zen VPN)

## Что такое VLESS+Reality?

**VLESS** - облегчённый прокси-протокол (без двойного шифрования)
**Reality** - transport layer который имитирует TLS подключение к реальному сайту

[Как работает Reality](https://objshadow.pages.dev/en/posts/how-reality-works/):
- Клиент подключается к серверу
- Сервер показывает **настоящий** TLS сертификат (например, apple.com)
- При активном зондировании GFW получает валидный ответ от apple.com
- Легитимный трафик проходит через реальный сайт, VPN трафик - через сервер

## Быстрая установка

### Вариант 1: Один скрипт (рекомендуется)

```bash
# На сервере (Ubuntu/Debian)
curl -sL https://raw.githubusercontent.com/crazypeace/xray-vless-reality/main/install.sh | bash

# Скрипт автоматически:
# - Установит Xray-core
# - Сгенерирует ключи
# - Настроит Reality с apple.com как target
# - Выдаст конфиг для клиента
```

### Вариант 2: Ручная установка

```bash
# 1. Установка Xray
bash -c "$(curl -L https://github.com/XTLS/Xray-install/raw/main/install-release.sh)" @ install

# 2. Генерация ключей Reality
xray x25519

# Output:
# Private key: <PRIVATE_KEY>
# Public key: <PUBLIC_KEY>

# 3. Генерация shortId
openssl rand -hex 8
# Output: <SHORT_ID>

# 4. Конфигурация сервера
sudo tee /usr/local/etc/xray/config.json > /dev/null <<'EOF'
{
  "log": {
    "loglevel": "warning"
  },
  "inbounds": [
    {
      "port": 443,
      "protocol": "vless",
      "settings": {
        "clients": [
          {
            "id": "YOUR_UUID_HERE",
            "flow": "xtls-rprx-vision"
          }
        ],
        "decryption": "none"
      },
      "streamSettings": {
        "network": "tcp",
        "security": "reality",
        "realitySettings": {
          "show": false,
          "dest": "www.apple.com:443",
          "serverNames": [
            "www.apple.com"
          ],
          "privateKey": "YOUR_PRIVATE_KEY",
          "shortIds": [
            "YOUR_SHORT_ID"
          ]
        }
      }
    }
  ],
  "outbounds": [
    {
      "protocol": "freedom",
      "tag": "direct"
    }
  ]
}
EOF

# 5. Генерация UUID для клиента
uuidgen
# Output: <UUID>

# 6. Замените значения в config.json:
# - YOUR_UUID_HERE → <UUID>
# - YOUR_PRIVATE_KEY → <PRIVATE_KEY>
# - YOUR_SHORT_ID → <SHORT_ID>

# 7. Запуск
sudo systemctl start xray
sudo systemctl enable xray
sudo systemctl status xray
```

## Конфигурация клиента

### Linux (Xray)

```bash
# Установка
bash -c "$(curl -L https://github.com/XTLS/Xray-install/raw/main/install-release.sh)" @ install

# Конфиг
sudo tee /usr/local/etc/xray/config.json > /dev/null <<'EOF'
{
  "log": {
    "loglevel": "warning"
  },
  "inbounds": [
    {
      "port": 10808,
      "protocol": "socks",
      "settings": {
        "udp": true
      }
    },
    {
      "port": 10809,
      "protocol": "http"
    }
  ],
  "outbounds": [
    {
      "protocol": "vless",
      "settings": {
        "vnext": [
          {
            "address": "YOUR_SERVER_IP",
            "port": 443,
            "users": [
              {
                "id": "YOUR_UUID",
                "encryption": "none",
                "flow": "xtls-rprx-vision"
              }
            ]
          }
        ]
      },
      "streamSettings": {
        "network": "tcp",
        "security": "reality",
        "realitySettings": {
          "serverName": "www.apple.com",
          "publicKey": "YOUR_PUBLIC_KEY",
          "shortId": "YOUR_SHORT_ID",
          "fingerprint": "chrome"
        }
      }
    }
  ]
}
EOF

# Запуск
sudo systemctl start xray

# Теперь у вас SOCKS5 proxy на localhost:10808
# и HTTP proxy на localhost:10809
```

### Настройка системного прокси

```bash
# Временно (текущая сессия)
export http_proxy=http://127.0.0.1:10809
export https_proxy=http://127.0.0.1:10809
export all_proxy=socks5://127.0.0.1:10808

# Проверка
curl ifconfig.me
```

### Или через TUN (как VPN)

```bash
# Установка tun2socks
go install github.com/xjasonlyu/tun2socks/v2@latest

# Создание TUN интерфейса
sudo ip tuntap add mode tun dev tun0
sudo ip addr add 198.18.0.1/15 dev tun0
sudo ip link set dev tun0 up

# Routing
sudo ip route add default via 198.18.0.1 dev tun0 table 100
sudo ip rule add from 198.18.0.0/15 table 100

# Запуск tun2socks
sudo tun2socks -device tun0 -proxy socks5://127.0.0.1:10808

# Теперь весь трафик идёт через VPN!
```

## Тестирование и сравнение

### Скрипт для тестирования обоих решений:

```bash
#!/bin/bash
# test-both-vpns.sh

echo "=== Тест Zen VPN ==="

# Запуск Zen VPN
sudo ./zen-client --config /etc/zen/clients/test.json &
ZEN_PID=$!
sleep 5

# Тесты
echo "IP: $(curl -s ifconfig.me)"
echo "Latency: $(ping -c 5 8.8.8.8 | tail -1)"
SPEED_ZEN=$(curl -w '%{speed_download}' -o /dev/null -s https://speed.cloudflare.com/__down?bytes=10000000)
echo "Speed: $SPEED_ZEN bytes/sec"

# Остановка
sudo kill $ZEN_PID

echo ""
echo "=== Тест VLESS+Reality ==="

# Запуск VLESS
sudo systemctl start xray
sleep 5

# Настройка прокси
export all_proxy=socks5://127.0.0.1:10808

# Тесты
echo "IP: $(curl -s ifconfig.me)"
echo "Latency: $(ping -c 5 8.8.8.8 | tail -1)"
SPEED_VLESS=$(curl -w '%{speed_download}' -o /dev/null -s https://speed.cloudflare.com/__down?bytes=10000000)
echo "Speed: $SPEED_VLESS bytes/sec"

# Сравнение
echo ""
echo "=== Сравнение ==="
echo "Zen VPN speed: $SPEED_ZEN bytes/sec"
echo "VLESS speed: $SPEED_VLESS bytes/sec"

if (( $(echo "$SPEED_VLESS > $SPEED_ZEN" | bc -l) )); then
    RATIO=$(echo "scale=2; $SPEED_VLESS / $SPEED_ZEN" | bc)
    echo "VLESS быстрее в $RATIO раз"
else
    RATIO=$(echo "scale=2; $SPEED_ZEN / $SPEED_VLESS" | bc)
    echo "Zen VPN быстрее в $RATIO раз"
fi
```

### Сохраните и запустите:

```bash
chmod +x test-both-vpns.sh
sudo ./test-both-vpns.sh
```

## Ожидаемые результаты

### Zen VPN:
```
Speed: ~100-500 KB/s
Latency: 150-300ms
Detection: ⚠️ Средняя (через behavioural analysis)
Whitelist bypass: ✅ Отлично (90%)
```

### VLESS+Reality:
```
Speed: ~10-50 MB/s
Latency: 50-100ms
Detection: ✅ Очень сложно (98-99%)
Whitelist bypass: ⚠️ Средне (60%)
```

## Активное зондирование (active probing test)

### Тест сервера на устойчивость к зондированию:

```bash
# Установка
go install github.com/Kkevsterrr/geneva@latest

# Проверка Zen VPN DNS сервера
sudo nmap -sV -p 53 YOUR_SERVER_IP

# Должен вернуть fake IP на случайные домены
dig @YOUR_SERVER_IP random-test-12345.vpn.example.com
# Response: 104.16.0.1 (fake IP)

# Проверка VLESS Reality сервера
openssl s_client -connect YOUR_SERVER_IP:443 -servername www.apple.com

# Должен вернуть НАСТОЯЩИЙ сертификат apple.com
# Certificate chain:
#  0 s:/CN=www.apple.com
#    i:/C=US/O=Apple Inc./CN=Apple Public EV Server RSA CA 2 - G1
```

## DPI эмуляция

```bash
# Установка Wireshark
sudo apt install wireshark tshark

# Захват трафика Zen VPN
sudo tshark -i any -f "port 443 and host dns.yandex.ru" -w zen-traffic.pcap

# В другом терминале: запустите Zen VPN и сделайте запросы

# Анализ
tshark -r zen-traffic.pcap -Y "dns" -T fields -e dns.qry.name | head -20
# Должны быть видны: api-users-auth-XXX.vpn.example.com

# Захват трафика VLESS
sudo tshark -i any -f "port 443 and host YOUR_SERVER_IP" -w vless-traffic.pcap

# Анализ
tshark -r vless-traffic.pcap -Y "tls.handshake.type == 1" -T fields -e tls.handshake.extensions_server_name
# Должно показать: www.apple.com
```

## Рекомендации

### Используйте Zen VPN когда:
1. **Провайдер использует whitelist**
   - Только Yandex/Google/VK разрешены
   - VLESS будет заблокирован по IP

2. **Нужна анонимность сервера**
   - IP сервера скрыт за DNS
   - Невозможно заблокировать по IP напрямую

3. **VLESS уже обнаружен**
   - Как fallback метод

### Используйте VLESS+Reality когда:
1. **Нужна скорость**
   - Streaming, gaming, large downloads
   - В 20-100 раз быстрее Zen VPN

2. **DPI активно используется**
   - GFW, TSPU, commercial DPI
   - 98-99% успешности

3. **Низкая латентность критична**
   - Trading, gaming, VoIP
   - В 2-3 раза ниже latency

### Hybrid подход (оптимально):
```bash
# Primary: VLESS+Reality (скорость)
# Fallback: Zen VPN (whitelist)

# В клиенте можно сделать авто-переключение:
if ! ping -c 1 -W 2 $VLESS_SERVER; then
    echo "VLESS недоступен, переключаюсь на Zen VPN"
    switch_to_zen_vpn
fi
```

## Дополнительные ресурсы

- [Reality protocol analysis](https://objshadow.pages.dev/en/posts/how-reality-works/)
- [VLESS vs Hysteria benchmarks](https://greatfirewallguide.com/lab/vless-reality-vision)
- [GFW bypass guide 2026](https://dev.to/mint_tea_592935ca2745ae07/bypassing-the-great-firewall-in-2026-active-filtering-protocol-obfuscation-37oj)
