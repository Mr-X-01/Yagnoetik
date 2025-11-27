#!/bin/bash

# Yagnoetik VPN - Автоматическая установка на Ubuntu 24.04
# Использование: ./install.sh your-domain.com admin@your-domain.com

set -e

# Цвета для вывода
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Функция для вывода сообщений
log() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

# Проверка аргументов
if [ $# -ne 2 ]; then
    error "Использование: $0 <domain> <email>"
fi

DOMAIN=$1
EMAIL=$2
API_KEY=$(openssl rand -hex 32)

log "🚀 Начинаем установку Yagnoetik VPN"
log "📍 Домен: $DOMAIN"
log "📧 Email: $EMAIL"
log "🔑 API ключ: $API_KEY"

# Проверка прав root
if [ "$EUID" -ne 0 ]; then
    error "Запустите скрипт с правами root: sudo $0 $DOMAIN $EMAIL"
fi

# Обновление системы
log "📦 Обновление системы..."
apt update && apt upgrade -y

# Установка зависимостей
log "📦 Установка зависимостей..."
apt update
apt install -y wget curl unzip nginx certbot python3-certbot-nginx ufw cron htop build-essential

# Установка Go 1.23
log "🐹 Установка Go 1.23..."
cd /tmp
wget https://go.dev/dl/go1.23.0.linux-amd64.tar.gz
rm -rf /usr/local/go
tar -C /usr/local -xzf go1.23.0.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile
export PATH=$PATH:/usr/local/go/bin

# Создание пользователя для VPN
log "👤 Создание пользователя yagnoetik..."
useradd -r -s /bin/false -d /opt/yagnoetik yagnoetik || true
mkdir -p /opt/yagnoetik
chown yagnoetik:yagnoetik /opt/yagnoetik

# Клонирование и сборка проекта
log "📥 Загрузка исходного кода из GitHub..."
cd /opt/yagnoetik
if [ -d "Yagnoetik" ]; then
    rm -rf Yagnoetik
fi

# Клонируем репозиторий
git clone https://github.com/Mr-X-01/Yagnoetik.git
if [ $? -ne 0 ]; then
    error "Не удалось клонировать репозиторий. Проверьте доступ к GitHub."
fi

# Устанавливаем права доступа
chown -R yagnoetik:yagnoetik Yagnoetik/

log "🔧 Настройка проекта..."

# Настройка Nginx
log "🌐 Настройка Nginx..."
cat > /etc/nginx/sites-available/yagnoetik << EOF
server {
    listen 80;
    server_name $DOMAIN;
    
    # Временная заглушка для получения сертификата
    location / {
        return 200 'Yagnoetik VPN Server Setup';
        add_header Content-Type text/plain;
    }
}
EOF

ln -sf /etc/nginx/sites-available/yagnoetik /etc/nginx/sites-enabled/
rm -f /etc/nginx/sites-enabled/default
nginx -t && systemctl reload nginx

# Получение SSL сертификата
log "🔒 Получение SSL сертификата от Let's Encrypt..."
certbot --nginx -d $DOMAIN --email $EMAIL --agree-tos --non-interactive --redirect

# Обновление конфигурации Nginx для проксирования
log "🔄 Обновление конфигурации Nginx..."
cat > /etc/nginx/sites-available/yagnoetik << EOF
# Yagnoetik VPN Server Configuration

# HTTP to HTTPS redirect
server {
    listen 80;
    server_name $DOMAIN;
    return 301 https://\$server_name\$request_uri;
}

# Main HTTPS server
server {
    listen 443 ssl http2;
    server_name $DOMAIN;
    
    # SSL Configuration
    ssl_certificate /etc/letsencrypt/live/$DOMAIN/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/$DOMAIN/privkey.pem;
    include /etc/letsencrypt/options-ssl-nginx.conf;
    ssl_dhparam /etc/letsencrypt/ssl-dhparams.pem;
    
    # Security headers
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;
    add_header X-Frame-Options DENY always;
    add_header X-Content-Type-Options nosniff always;
    add_header X-XSS-Protection "1; mode=block" always;
    
    # Proxy to Yagnoetik server
    location / {
        proxy_pass https://127.0.0.1:8444;
        proxy_ssl_verify off;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        
        # gRPC support
        grpc_pass grpc://127.0.0.1:8444;
        grpc_set_header Host \$host;
    }
}

# Admin panel
server {
    listen 8080 ssl;
    server_name $DOMAIN;
    
    ssl_certificate /etc/letsencrypt/live/$DOMAIN/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/$DOMAIN/privkey.pem;
    
    location / {
        proxy_pass http://127.0.0.1:8081;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }
}
EOF

nginx -t && systemctl reload nginx

# Создание systemd сервисов
log "⚙️ Создание systemd сервисов..."

# Сервис для основного сервера
cat > /etc/systemd/system/yagnoetik-server.service << EOF
[Unit]
Description=Yagnoetik VPN Server
After=network.target
Wants=network.target

[Service]
Type=simple
User=yagnoetik
Group=yagnoetik
WorkingDirectory=/opt/yagnoetik/Yagnoetik/server
ExecStart=/opt/yagnoetik/Yagnoetik/server/yagnoetik-server
Environment=API_KEY=$API_KEY
Environment=TLS_CERT=/etc/letsencrypt/live/$DOMAIN/fullchain.pem
Environment=TLS_KEY=/etc/letsencrypt/live/$DOMAIN/privkey.pem
Environment=DOMAIN=$DOMAIN
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=yagnoetik-server

# Security settings
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/opt/yagnoetik

[Install]
WantedBy=multi-user.target
EOF

# Сервис для админ-панели
cat > /etc/systemd/system/yagnoetik-admin.service << EOF
[Unit]
Description=Yagnoetik VPN Admin Panel
After=network.target yagnoetik-server.service
Wants=network.target

[Service]
Type=simple
User=yagnoetik
Group=yagnoetik
WorkingDirectory=/opt/yagnoetik/Yagnoetik/admin-panel
ExecStart=/opt/yagnoetik/Yagnoetik/admin-panel/yagnoetik-admin
Environment=SERVER_URL=http://localhost:8443
Environment=API_KEY=$API_KEY
Environment=PORT=8081
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=yagnoetik-admin

# Security settings
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true

[Install]
WantedBy=multi-user.target
EOF

# Настройка файрвола
log "🔥 Настройка файрвола..."
ufw --force reset
ufw default deny incoming
ufw default allow outgoing
ufw allow ssh
ufw allow 80/tcp
ufw allow 443/tcp
ufw allow 8080/tcp
ufw --force enable

# Создание конфигурационного файла
log "📝 Создание конфигурации..."
cat > /opt/yagnoetik/config.env << EOF
# Yagnoetik VPN Configuration
DOMAIN=$DOMAIN
API_KEY=$API_KEY
TLS_CERT=/etc/letsencrypt/live/$DOMAIN/fullchain.pem
TLS_KEY=/etc/letsencrypt/live/$DOMAIN/privkey.pem
SERVER_PORT=8444
ADMIN_PORT=8443
ADMIN_PANEL_PORT=8081
EOF

chown yagnoetik:yagnoetik /opt/yagnoetik/config.env
chmod 600 /opt/yagnoetik/config.env

# Генерация protobuf файлов
log "🔧 Генерация protobuf файлов..."
cd /opt/yagnoetik/Yagnoetik

# Установка protoc-gen-go плагинов с правильным PATH
export GOPATH=/opt/yagnoetik/go
export PATH=$PATH:/usr/local/go/bin:$GOPATH/bin
mkdir -p $GOPATH

sudo -u yagnoetik bash -c "
export GOPATH=/opt/yagnoetik/go
export PATH=\$PATH:/usr/local/go/bin:\$GOPATH/bin
mkdir -p \$GOPATH
/usr/local/go/bin/go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
/usr/local/go/bin/go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
"

# Установка protoc
if ! command -v protoc &> /dev/null; then
    log "📦 Установка protoc..."
    PROTOC_VERSION="25.1"
    cd /tmp
    wget https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-linux-x86_64.zip
    unzip protoc-${PROTOC_VERSION}-linux-x86_64.zip -d protoc
    cp protoc/bin/protoc /usr/local/bin/
    cp -r protoc/include/* /usr/local/include/
    chmod +x /usr/local/bin/protoc
    rm -rf protoc protoc-${PROTOC_VERSION}-linux-x86_64.zip
fi

# Генерация protobuf для сервера
cd /opt/yagnoetik/Yagnoetik/server
sudo -u yagnoetik bash -c "
export GOPATH=/opt/yagnoetik/go
export PATH=\$PATH:/usr/local/go/bin:\$GOPATH/bin
protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    proto/tunnel.proto
"

# Генерация protobuf для Windows клиента
cd /opt/yagnoetik/Yagnoetik/client-windows
sudo -u yagnoetik bash -c "
export GOPATH=/opt/yagnoetik/go
export PATH=\$PATH:/usr/local/go/bin:\$GOPATH/bin
protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    proto/tunnel.proto
"

# Создание скрипта для сборки
cat > /opt/yagnoetik/build.sh << 'EOF'
#!/bin/bash
cd /opt/yagnoetik/Yagnoetik

# Сборка сервера
echo "🔨 Сборка сервера..."
cd server
/usr/local/go/bin/go mod tidy
/usr/local/go/bin/go build -o yagnoetik-server ./cmd/server
chmod +x yagnoetik-server

# Сборка админ-панели
echo "🔨 Сборка админ-панели..."
cd ../admin-panel
/usr/local/go/bin/go mod tidy
/usr/local/go/bin/go build -o yagnoetik-admin .
chmod +x yagnoetik-admin

# Сборка Windows клиента (опционально)
echo "🔨 Сборка Windows клиента..."
cd ../client-windows
/usr/local/go/bin/go mod tidy
GOOS=windows GOARCH=amd64 /usr/local/go/bin/go build -o yagnoetik-windows-client.exe ./cmd
chmod +x yagnoetik-windows-client.exe

echo "✅ Сборка завершена!"
EOF

chmod +x /opt/yagnoetik/build.sh
chown yagnoetik:yagnoetik /opt/yagnoetik/build.sh

# Создание скрипта управления
cat > /usr/local/bin/yagnoetik << 'EOF'
#!/bin/bash

case "$1" in
    start)
        systemctl start yagnoetik-server yagnoetik-admin
        echo "Yagnoetik VPN запущен"
        ;;
    stop)
        systemctl stop yagnoetik-server yagnoetik-admin
        echo "Yagnoetik VPN остановлен"
        ;;
    restart)
        systemctl restart yagnoetik-server yagnoetik-admin
        echo "Yagnoetik VPN перезапущен"
        ;;
    status)
        systemctl status yagnoetik-server yagnoetik-admin
        ;;
    logs)
        journalctl -f -u yagnoetik-server -u yagnoetik-admin
        ;;
    build)
        sudo -u yagnoetik /opt/yagnoetik/build.sh
        ;;
    *)
        echo "Использование: $0 {start|stop|restart|status|logs|build}"
        exit 1
        ;;
esac
EOF

chmod +x /usr/local/bin/yagnoetik

# Настройка автообновления сертификатов
log "🔄 Настройка автообновления сертификатов..."
(crontab -l 2>/dev/null; echo "0 12 * * * /usr/bin/certbot renew --quiet && systemctl reload nginx") | crontab -

# Включение сервисов
systemctl daemon-reload
systemctl enable yagnoetik-server yagnoetik-admin

log "✅ Установка завершена!"
echo
echo -e "${BLUE}=== ИНФОРМАЦИЯ О СИСТЕМЕ ===${NC}"
echo -e "🌐 Домен: ${GREEN}$DOMAIN${NC}"
echo -e "🔑 API ключ: ${GREEN}$API_KEY${NC}"
echo -e "📊 Админ-панель: ${GREEN}https://$DOMAIN:8080${NC}"
echo -e "🔒 Основной сервер: ${GREEN}https://$DOMAIN${NC}"
echo
echo -e "${BLUE}=== КОМАНДЫ УПРАВЛЕНИЯ ===${NC}"
echo -e "▶️  Запуск: ${GREEN}yagnoetik start${NC}"
echo -e "⏹️  Остановка: ${GREEN}yagnoetik stop${NC}"
echo -e "🔄 Перезапуск: ${GREEN}yagnoetik restart${NC}"
echo -e "📊 Статус: ${GREEN}yagnoetik status${NC}"
echo -e "📝 Логи: ${GREEN}yagnoetik logs${NC}"
echo -e "🔨 Сборка: ${GREEN}yagnoetik build${NC}"
echo
echo -e "${YELLOW}⚠️  ВАЖНО: Скопируйте исходный код в /opt/yagnoetik/Yagnoetik/ и выполните 'yagnoetik build'${NC}"
echo -e "${YELLOW}⚠️  Сохраните API ключ в безопасном месте!${NC}"
