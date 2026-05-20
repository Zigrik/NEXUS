# Nexus - Tunnel System

Система туннелирования трафика между VPS и Raspberry Pi. Позволяет публиковать локальные сервисы (чат, API, игры) через облачный сервер.

## 🏗 Архитектура
Клиент → VPS (Nexus) → Туннель → Raspberry Pi → Docker контейнеры

text

- **VPS**: принимает запросы, маршрутизирует по субдоменам
- **Raspberry Pi**: подключается к VPS, перенаправляет трафик на локальные сервисы

## 🚀 Быстрый старт на VPS

### 1. Открыть порты в фаерволе

```bash
sudo ufw allow 22/tcp     # SSH
sudo ufw allow 80/tcp     # HTTP (для Certbot)
sudo ufw allow 443/tcp    # HTTPS
sudo ufw allow 8443/tcp   # Nexus Gateway
sudo ufw allow 9000/tcp   # Nexus Control
sudo ufw enable
2. Настроить DNS
В панели управления доменом добавьте A-записи:

text
zilab.su          A    IP_вашего_VPS
chat.zilab.su     A    IP_вашего_VPS
launcher.zilab.su A    IP_вашего_VPS
api.zilab.su      A    IP_вашего_VPS
auth.zilab.su     A    IP_вашего_VPS
pay.zilab.su      A    IP_вашего_VPS
3. Получить SSL сертификаты
bash
sudo apt install certbot -y
sudo certbot certonly --standalone \
  -d zilab.su -d chat.zilab.su -d launcher.zilab.su \
  -d api.zilab.su -d auth.zilab.su -d pay.zilab.su \
  --email admin@zilab.su --agree-tos
4. Запустить Nexus
bash
cd /opt/nexus
sudo docker-compose up -d
5. Настроить Nginx (прокси на Nexus)
nginx
server {
    listen 443 ssl http2;
    server_name *.zilab.su;
    
    ssl_certificate /etc/letsencrypt/live/zilab.su/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/zilab.su/privkey.pem;
    
    location / {
        proxy_pass https://127.0.0.1:8443;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
📁 Файл конфигурации (config.yaml)
yaml
control:
  port: 9000              # Порт для подключения Raspberry Pi

gateway:
  port: 8443              # Порт для HTTP/HTTPS запросов
  https: true
  cert_file: "/etc/letsencrypt/live/zilab.su/fullchain.pem"
  key_file: "/etc/letsencrypt/live/zilab.su/privkey.pem"

routes:
  - host: "chat.zilab.su"
    device_id: "raspberry-pi-4"
    target_port: 8080     # Порт сервиса на Raspberry Pi

static:
  enabled: true
  domain: "zilab.su"
  path: "./web"
  index_file: "index.html"
🖥 Команды управления
bash
# Запуск
sudo docker-compose up -d

# Остановка
sudo docker-compose down

# Логи
sudo docker-compose logs -f

# Перезапуск
sudo docker-compose restart

# Обновление (после git pull)
sudo docker-compose build --no-cache
sudo docker-compose up -d
🔍 Проверка работы
bash
# Health check
curl https://zilab.su/health

# Список маршрутов
curl https://zilab.su/routes

# Статический сайт (index.html)
curl https://zilab.su/
📦 Структура проекта
text
nexus/
├── cmd/nexusd/          # Точка входа
├── internal/            # Внутренние пакеты
│   ├── config/         # Конфигурация
│   ├── control/        # Управление сессиями
│   ├── gateway/        # HTTP прокси
│   └── static/         # Статические файлы
├── pkg/                 # Публичные пакеты
│   ├── protocol/       # Бинарный протокол
│   └── logger/         # Логирование
├── web/                 # Статические файлы (index.html)
├── config.yaml          # Конфигурация
└── docker-compose.yml   # Docker compose
🛠 Требования
Go 1.21+

Docker & Docker Compose

SSL сертификаты (Let's Encrypt)

📝 Лицензия
MIT