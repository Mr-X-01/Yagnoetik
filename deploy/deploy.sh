#!/bin/bash

# Yagnoetik VPN - Скрипт развертывания на сервере
# Использование: ./deploy.sh your-domain.com admin@your-domain.com

set -e

DOMAIN=$1
EMAIL=$2

if [ $# -ne 2 ]; then
    echo "Использование: $0 <domain> <email>"
    exit 1
fi

echo "🚀 Развертывание Yagnoetik VPN на сервере"
echo "📍 Домен: $DOMAIN"
echo "📧 Email: $EMAIL"

# Создание архива с исходным кодом
echo "📤 Развертывание на сервере..."
echo "Выполните следующие команды на вашем Ubuntu 24 сервере:"
echo
echo "# 1. Загрузите скрипт установки на сервер:"
echo "wget https://raw.githubusercontent.com/Mr-X-01/Yagnoetik/main/deploy/install.sh"
echo
echo "# 2. Запустите установку:"
echo "chmod +x install.sh"
echo "sudo ./install.sh $DOMAIN $EMAIL"
echo
echo "✅ После выполнения этих команд ваш VPN будет доступен по адресу:"
echo "🌐 Основной сервер: https://$DOMAIN"
echo "📊 Админ-панель: https://$DOMAIN:8080"
