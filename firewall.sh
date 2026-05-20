#!/bin/bash
# firewall.sh - настройка фаервола для Nexus

# Сброс правил (осторожно!)
# sudo ufw reset

# Базовые правила
sudo ufw default deny incoming
sudo ufw default allow outgoing

# SSH
sudo ufw allow 22/tcp

# HTTP/HTTPS
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp

# Nexus
sudo ufw allow 8443/tcp
sudo ufw allow 9000/tcp

# x-ui
sudo ufw allow 54322/tcp
sudo ufw allow 54333/tcp

# Включаем
sudo ufw enable

# Статус
sudo ufw status verbose