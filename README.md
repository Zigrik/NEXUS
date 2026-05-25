# Nexus - Tunnel System

Система туннелирования трафика между VPS и Raspberry Pi с mTLS аутентификацией.

## 🏗 Архитектура
Клиент → VPS (Nexus) → mTLS Tunnel → Raspberry Pi → Docker контейнеры

text

- **VPS**: принимает запросы, маршрутизирует по субдоменам, mTLS защита
- **Raspberry Pi**: подключается к VPS с сертификатом, перенаправляет на локальные сервисы

## 🔐 Безопасность

- **mTLS** на порту 9000 (взаимная аутентификация)
- **HTTPS** на порту 8443 (Let's Encrypt)
- Клиентские сертификаты для каждого устройства

## 🚀 Быстрый старт на VPS

### 1. Открыть порты

```bash
sudo ufw allow 22/tcp     # SSH
sudo ufw allow 80/tcp     # HTTP (для Certbot)
sudo ufw allow 443/tcp    # HTTPS
sudo ufw allow 8443/tcp   # Nexus Gateway
sudo ufw allow 9000/tcp   # Nexus Control (mTLS)
sudo ufw enable
2. Клонирование и настройка
bash
cd /opt
sudo git clone https://github.com/Zigrik/nexus.git
cd nexus

# Обновить config.yaml под свои домены
sudo nano config.yaml
3. SSL сертификаты (для HTTPS)
bash
sudo certbot certonly --standalone \
  -d zilab.su -d chat.zilab.su -d launcher.zilab.su \
  -d api.zilab.su -d auth.zilab.su -d pay.zilab.su \
  --email admin@zilab.su --agree-tos
4. mTLS сертификаты (для Control порта)
bash
# Создаем CA и сертификаты
mkdir -p certs && cd certs

# CA (10 лет)
openssl genrsa -out ca-key.pem 4096
openssl req -new -x509 -days 3650 -key ca-key.pem -sha256 -out ca.pem \
  -subj "/CN=Nexus CA"

# Серверный сертификат
openssl genrsa -out server-key.pem 4096
openssl req -subj "/CN=195.78.49.38" -sha256 -new \
  -key server-key.pem -out server.csr
openssl x509 -req -days 365 -sha256 -in server.csr \
  -CA ca.pem -CAkey ca-key.pem -CAcreateserial -out server-cert.pem

# Клиентский сертификат (Raspberry Pi)
openssl genrsa -out client-raspberry-key.pem 4096
openssl req -subj "/CN=raspberry-pi-4" -sha256 -new \
  -key client-raspberry-key.pem -out client-raspberry.csr
openssl x509 -req -days 365 -sha256 -in client-raspberry.csr \
  -CA ca.pem -CAkey ca-key.pem -CAcreateserial -out client-raspberry-cert.pem

chmod 600 *.key.pem
cd ..
5. Запуск Nexus
bash
sudo docker-compose build --no-cache
sudo docker-compose up -d
sudo docker-compose ps
6. Настройка Nginx (прокси на 8443)
nginx
server {
    listen 443 ssl http2;
    server_name zilab.su *.zilab.su;
    
    ssl_certificate /etc/letsencrypt/live/zilab.su/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/zilab.su/privkey.pem;
    
    location / {
        proxy_pass https://127.0.0.1:8443;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
🖥 Настройка Raspberry Pi агента
1. Копирование сертификатов с VPS
bash
scp root@195.78.49.38:/opt/nexus/certs/ca.pem ~/nexus-agent/certs/
scp root@195.78.49.38:/opt/nexus/certs/client-raspberry-cert.pem ~/nexus-agent/certs/client-cert.pem
scp root@195.78.49.38:/opt/nexus/certs/client-raspberry-key.pem ~/nexus-agent/certs/client-key.pem
2. Конфигурация агента (config.yaml)
yaml
vps:
  address: "195.78.49.38:9000"

tls:
  enabled: true
  ca_file: "./certs/ca.pem"
  cert_file: "./certs/client-cert.pem"
  key_file: "./certs/client-key.pem"

device:
  id: "raspberry-pi-4"
  name: "Raspberry Pi 4"
  version: "1.0.0"

heartbeat:
  interval: 30
  timeout: 90

request:
  timeout: 60

log:
  level: "info"
  encoding: "console"
3. Запуск
bash
cd ~/nexus-agent
chmod +x nexus-agent
./nexus-agent
4. Systemd сервис (автозапуск)
bash
sudo nano /etc/systemd/system/nexus-agent.service
ini
[Unit]
Description=Nexus Agent
After=network.target

[Service]
Type=simple
User=pi
WorkingDirectory=/home/pi/nexus-agent
ExecStart=/home/pi/nexus-agent/nexus-agent
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
bash
sudo systemctl daemon-reload
sudo systemctl enable nexus-agent
sudo systemctl start nexus-agent
🔧 Команды управления
bash
# VPS
cd /opt/nexus
sudo docker-compose logs -f      # Логи
sudo docker-compose restart       # Перезапуск
sudo docker-compose down          # Остановка
sudo docker-compose up -d         # Запуск

# Агент
sudo systemctl status nexus-agent
sudo systemctl restart nexus-agent
sudo journalctl -u nexus-agent -f
🔍 Проверка работы
bash
# Health check
curl https://zilab.su/health

# Список маршрутов
curl https://zilab.su/routes

# Статика
curl https://zilab.su/

# Проверка mTLS на VPS
sudo docker-compose logs nexus | grep "Client authenticated"
📁 Структура проекта
text
nexus/
├── cmd/nexusd/           # Точка входа
├── internal/
│   ├── config/          # Конфигурация
│   ├── control/         # Управление сессиями (mTLS)
│   ├── gateway/         # HTTP прокси
│   └── static/          # Статические файлы
├── pkg/
│   ├── protocol/        # Бинарный протокол
│   └── logger/          # Логирование
├── certs/               # mTLS сертификаты
├── web/                 # Статика (index.html)
├── config.yaml
└── docker-compose.yml
🔄 Обновление сертификатов
bash
# Ежегодное обновление
cd /opt/nexus/certs
./renew-certs.sh

# Перезапуск Nexus
cd .. && sudo docker-compose restart
📝 Лицензия
MIT

text

Сохраните как `README.md` в корне проекта и запушите в репозиторий:

```bash
cd /opt/nexus
git add README.md
git commit -m "docs: update README with mTLS setup"
git push origin main