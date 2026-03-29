#!/bin/bash
# Скрипт для сравнения производительности Zen VPN и VLESS+Reality

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "========================================"
echo "  Zen VPN vs VLESS+Reality Comparison"
echo "========================================"
echo ""

# Проверка прав
if [ "$EUID" -ne 0 ]; then
    echo -e "${RED}Запустите с sudo${NC}"
    exit 1
fi

# Функция для остановки всего
cleanup() {
    echo -e "\n${YELLOW}Очистка...${NC}"
    pkill -f zen-client 2>/dev/null || true
    systemctl stop xray 2>/dev/null || true
    unset http_proxy https_proxy all_proxy
}

trap cleanup EXIT

# Функция для измерения скорости
measure_speed() {
    local name=$1
    echo -e "${GREEN}Тестирование $name...${NC}"

    # IP адрес
    echo -n "  IP: "
    IP=$(timeout 10 curl -s ifconfig.me 2>/dev/null || echo "FAILED")
    echo "$IP"

    # Latency (ping)
    echo -n "  Latency: "
    LATENCY=$(timeout 10 ping -c 5 8.8.8.8 2>/dev/null | tail -1 | awk '{print $4}' | cut -d '/' -f 2 || echo "FAILED")
    echo "${LATENCY}ms"

    # Download speed (10MB test)
    echo -n "  Download speed: "
    SPEED=$(timeout 30 curl -w '%{speed_download}' -o /dev/null -s https://speed.cloudflare.com/__down?bytes=10000000 2>/dev/null || echo "0")
    SPEED_MBPS=$(echo "scale=2; $SPEED / 1024 / 1024" | bc -l 2>/dev/null || echo "0")
    echo "${SPEED_MBPS} MB/s"

    # DNS resolution time
    echo -n "  DNS resolution: "
    DNS_TIME=$(timeout 10 bash -c 'time host google.com 2>&1' 2>&1 | grep real | awk '{print $2}' || echo "FAILED")
    echo "$DNS_TIME"

    echo ""

    # Возвращаем результаты
    echo "$IP|$LATENCY|$SPEED_MBPS|$DNS_TIME"
}

# Проверка базового подключения
echo -e "${YELLOW}[1/4] Проверка базового подключения...${NC}"
BASELINE=$(measure_speed "Baseline (без VPN)")

# Тест Zen VPN
echo -e "${YELLOW}[2/4] Тестирование Zen VPN...${NC}"

if [ ! -f "./zen-client" ]; then
    echo -e "${RED}zen-client не найден. Соберите проект: make build${NC}"
    exit 1
fi

if [ ! -f "/etc/zen/clients/test.json" ]; then
    echo -e "${RED}Конфиг клиента не найден. Создайте: zenctl add-client test${NC}"
    exit 1
fi

echo "  Запуск Zen VPN клиента..."
./zen-client --config /etc/zen/clients/test.json > /tmp/zen-vpn.log 2>&1 &
ZEN_PID=$!

sleep 10  # Ждём подключения

if ! ps -p $ZEN_PID > /dev/null; then
    echo -e "${RED}Zen VPN не запустился. Проверьте /tmp/zen-vpn.log${NC}"
    exit 1
fi

ZEN_RESULTS=$(measure_speed "Zen VPN")
kill $ZEN_PID 2>/dev/null || true
sleep 2

# Тест VLESS+Reality
echo -e "${YELLOW}[3/4] Тестирование VLESS+Reality...${NC}"

if ! command -v xray &> /dev/null; then
    echo -e "${YELLOW}Xray не установлен. Пропускаем тест VLESS.${NC}"
    VLESS_RESULTS="N/A|N/A|N/A|N/A"
else
    if [ ! -f "/usr/local/etc/xray/config.json" ]; then
        echo -e "${RED}Конфиг Xray не найден: /usr/local/etc/xray/config.json${NC}"
        VLESS_RESULTS="N/A|N/A|N/A|N/A"
    else
        echo "  Запуск VLESS+Reality..."
        systemctl start xray
        sleep 5

        export all_proxy=socks5://127.0.0.1:10808

        VLESS_RESULTS=$(measure_speed "VLESS+Reality")

        systemctl stop xray
        unset all_proxy
    fi
fi

# Результаты
echo -e "${YELLOW}[4/4] Сводная таблица результатов${NC}"
echo ""
echo "╔═══════════════════╦════════════════╦═════════════╦═══════════════╦═══════════════╗"
echo "║ Метод             ║ IP (last 8)    ║ Latency     ║ Speed         ║ DNS Time      ║"
echo "╠═══════════════════╬════════════════╬═════════════╬═══════════════╬═══════════════╣"

print_row() {
    local name=$1
    local data=$2

    IFS='|' read -r ip latency speed dns <<< "$data"

    # Обрезаем IP до последних 8 символов
    if [ ${#ip} -gt 8 ]; then
        ip="...${ip: -8}"
    fi

    printf "║ %-17s ║ %-14s ║ %-11s ║ %-13s ║ %-13s ║\n" \
        "$name" "$ip" "${latency}ms" "${speed} MB/s" "$dns"
}

print_row "Baseline" "$BASELINE"
print_row "Zen VPN" "$ZEN_RESULTS"
print_row "VLESS+Reality" "$VLESS_RESULTS"

echo "╚═══════════════════╩════════════════╩═════════════╩═══════════════╩═══════════════╝"
echo ""

# Анализ
IFS='|' read -r _ baseline_latency baseline_speed _ <<< "$BASELINE"
IFS='|' read -r _ zen_latency zen_speed _ <<< "$ZEN_RESULTS"
IFS='|' read -r _ vless_latency vless_speed _ <<< "$VLESS_RESULTS"

echo -e "${GREEN}Анализ:${NC}"

if [ "$zen_speed" != "0" ] && [ "$baseline_speed" != "0" ]; then
    zen_ratio=$(echo "scale=2; $zen_speed / $baseline_speed * 100" | bc -l)
    echo "  - Zen VPN: ${zen_ratio}% от базовой скорости"
fi

if [ "$vless_speed" != "N/A" ] && [ "$vless_speed" != "0" ] && [ "$baseline_speed" != "0" ]; then
    vless_ratio=$(echo "scale=2; $vless_speed / $baseline_speed * 100" | bc -l)
    echo "  - VLESS+Reality: ${vless_ratio}% от базовой скорости"
fi

if [ "$vless_speed" != "N/A" ] && [ "$vless_speed" != "0" ] && [ "$zen_speed" != "0" ]; then
    diff_ratio=$(echo "scale=1; $vless_speed / $zen_speed" | bc -l)
    echo "  - VLESS быстрее Zen VPN в ${diff_ratio}x раз"
fi

echo ""
echo -e "${YELLOW}Рекомендации:${NC}"

if (( $(echo "$zen_speed < 0.5" | bc -l) )); then
    echo "  ⚠️  Zen VPN: низкая скорость (<0.5 MB/s). Рекомендуется для whitelist обхода."
else
    echo "  ✅ Zen VPN: приемлемая скорость для базового использования."
fi

if [ "$vless_speed" != "N/A" ]; then
    if (( $(echo "$vless_speed > 5" | bc -l) )); then
        echo "  ✅ VLESS+Reality: отличная скорость. Рекомендуется для streaming/gaming."
    elif (( $(echo "$vless_speed > 1" | bc -l) )); then
        echo "  ✅ VLESS+Reality: хорошая скорость для общего использования."
    else
        echo "  ⚠️  VLESS+Reality: низкая скорость. Проверьте конфигурацию."
    fi
fi

echo ""
echo "Логи сохранены в /tmp/zen-vpn.log"
echo "Для детального анализа запустите: journalctl -u xray -n 50"
