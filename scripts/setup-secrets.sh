#!/bin/bash

# Проверка наличия gh CLI
if ! command -v gh &> /dev/null; then
    echo "Установите GitHub CLI: https://cli.github.com/"
    exit 1
fi

# Проверка авторизации
if ! gh auth status &> /dev/null; then
    echo "Авторизуйтесь в GitHub CLI: gh auth login"
    exit 1
fi

# Получение репозитория
REPO=$(gh repo view --json nameWithOwner -q .nameWithOwner)

echo "Настройка секретов для репозитория: $REPO"

# Запрос данных
read -p "Docker Hub Username: " DOCKERHUB_USERNAME
read -s -p "Docker Hub Token: " DOCKERHUB_TOKEN
echo
read -p "Production Host (IP/Domain): " PRODUCTION_HOST
read -p "Production Username: " PRODUCTION_USERNAME
read -s -p "Production SSH Key (вставьте содержимое ключа): " PRODUCTION_SSH_KEY
echo

# Установка секретов
echo "Установка секретов..."
gh secret set DOCKERHUB_USERNAME -b"$DOCKERHUB_USERNAME"
gh secret set DOCKERHUB_TOKEN -b"$DOCKERHUB_TOKEN"
gh secret set PRODUCTION_HOST -b"$PRODUCTION_HOST"
gh secret set PRODUCTION_USERNAME -b"$PRODUCTION_USERNAME"
gh secret set PRODUCTION_SSH_KEY -b"$PRODUCTION_SSH_KEY"

echo "Секреты успешно установлены!" 