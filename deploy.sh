#!/bin/bash
# deploy.sh - скрипт для быстрого деплоя

set -e

echo "🚀 Начинаем деплой Nexus..."

# Проверяем сертификаты
if [ ! -f "/etc/letsencrypt/live/zilab.su/fullchain.pem" ]; then
    echo "❌ SSL сертификаты не найдены!"
    exit 1
fi

# Открываем порты в фаерволе
echo "🔓 Открываем порты в фаерволе..."
sudo ufw allow 8443/tcp
sudo ufw allow 9000/tcp

# Копируем nginx конфиг
echo "📝 Настраиваем nginx..."
sudo cp nginx.conf /etc/nginx/sites-available/zilab.su
sudo ln -sf /etc/nginx/sites-available/zilab.su /etc/nginx/sites-enabled/
sudo nginx -t && sudo systemctl restart nginx

# Собираем и запускаем Nexus
echo "🐳 Запускаем Nexus..."
cd /opt/nexus
sudo docker-compose down
sudo docker-compose build
sudo docker-compose up -d

# Проверяем
echo "✅ Проверяем работу..."
sleep 5
curl -k https://localhost:8443/health

echo "🎉 Деплой завершен!"
echo "Домен: https://zilab.su"
echo "Health check: https://zilab.su/health"