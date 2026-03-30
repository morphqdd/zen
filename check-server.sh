#!/bin/bash
# Скрипт для проверки что сервер работает

echo "🔍 Проверка Zen VPN сервера..."
echo ""

# 1. Проверка через DoH
echo "1️⃣ Проверка через Google DoH:"
curl -s -H "accept: application/dns-json" \
  "https://dns.google/resolve?name=test-$(date +%s).tunnel.qtrack.click&type=A" | \
  jq -r '.Status, .Comment // "OK"'

echo ""

# 2. Проверка NS записи
echo "2️⃣ Проверка NS записи:"
curl -s -H "accept: application/dns-json" \
  "https://dns.google/resolve?name=tunnel.qtrack.click&type=NS" | \
  jq -r '.Answer[].data // "NO NS"'

echo ""

# 3. Проверка через Yandex DoH
echo "3️⃣ Проверка через Yandex DoH:"
curl -s -H "accept: application/dns-json" \
  "https://dns.yandex.ru/dns-query?name=api-test-123.tunnel.qtrack.click&type=A" | \
  jq -r '.Answer[].data // "NO ANSWER"'

echo ""
echo "✅ Если видите IP адреса - сервер работает!"
echo "❌ Если ошибки - сервер не запущен или недоступен"
