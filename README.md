# Rich - Инструмент обогащения markdown-контента с помощью AI

[![Go Version](https://img.shields.io/github/go-mod/go-version/Headcrab/rich)](https://go.dev)
[![License](https://img.shields.io/github/license/Headcrab/rich)](LICENSE)
[![Coverage](https://codecov.io/gh/Headcrab/rich/graph/badge.svg?token=WSRWMHXMTA)](https://codecov.io/gh/Headcrab/rich)

Rich - это утилита командной строки, разработанная на Go, которая автоматически обогащает markdown-файлы с использованием различных моделей искусственного интеллекта (GPT, Claude, OpenRouter). Инструмент анализирует содержимое markdown-файлов и дополняет их детальной информацией, ссылками и пояснениями.

## Возможности

- 🤖 Поддержка различных AI-моделей (OpenAI, Anthropic Claude, OpenRouter)
- 📁 Пакетная обработка markdown-файлов
- ⚙️ Гибкая настройка через конфигурационный файл
- 🔄 Сохранение оригинальной структуры документов
- 🎯 Настраиваемые промпты для точной генерации контента
- 🚫 Автоматическое исключение обработанных файлов
- 🔒 Безопасная обработка файлов с проверкой размера и содержимого
- 📊 Ограничение частоты запросов к API (rate limiting)

## Установка

```bash
go install github.com/Headcrab/rich@latest
```

Или сборка из исходного кода:

```bash
git clone https://github.com/Headcrab/rich.git
cd rich
go build
```

### Сборка с помощью Task

Для удобства разработки в проекте используется [Task](https://taskfile.dev/) - современная альтернатива Make.

#### Установка Task

```bash
# Windows (Chocolatey)
choco install go-task

# Windows (Scoop)  
scoop install task

# macOS (Homebrew)
brew install go-task/tap/go-task

# Linux (Snap)
sudo snap install task --classic

# Или через Go
go install github.com/go-task/task/v3/cmd/task@latest
```

#### Доступные команды

```bash
# Показать все доступные задачи
task

# Настройка проекта (создание директорий)
task setup

# Сборка приложения
task build

# Запуск тестов
task test

# Запуск с покрытием кода
task test-coverage

# Форматирование кода
task fmt

# Статический анализ
task vet

# Полная проверка (fmt + vet + test)
task check

# Очистка артефактов сборки
task clean

# Очистка логов
task clean-logs

# Полная очистка
task clean-all

# Запуск приложения
task run

# Сборка и запуск
task run-build
```

## Конфигурация

Создайте файл `rich.cfg` в директории проекта:

```ini
[DIRECTORIES]
input_dir  = ./todo    # Директория с исходными файлами
output_dir = ./done    # Директория для обработанных файлов

[EXCLUSIONS]
excluded_files = README.md, CHANGELOG.md, LICENSE.md

[MODEL]
name        = gpt-3.5-turbo
api_url     = https://api.openai.com/v1/chat/completions
api_key_env = OPENAI_API_KEY    # Переменная окружения для API ключа
temperature = 0.7
max_tokens  = 1000

[PROMPT]
text = """Ваш промпт для обогащения контента"""
```

### Поддерживаемые API

Rich автоматически определяет формат запроса на основе URL API:

- **OpenAI**: `https://api.openai.com/v1/chat/completions`
- **Anthropic**: `https://api.anthropic.com/v1/messages`
- **OpenRouter**: `https://openrouter.ai/api/v1/chat/completions`

Ключи API могут быть указаны напрямую в конфигурации или через переменные окружения:

- `OPENAI_API_KEY` - для OpenAI
- `ANTHROPIC_API_KEY` - для Anthropic
- `OPENROUTER_API_KEY` - для OpenRouter

## Использование

1. Подготовьте markdown-файлы в директории `input_dir`
2. Настройте параметры в `rich.cfg`
3. Запустите обработку:

```bash
./rich -config rich.cfg
```

### Параметры командной строки

- `-config` - путь к конфигурационному файлу (по умолчанию: rich.cfg)

### Ограничения

- Максимальный размер обрабатываемого файла: 10 МБ
- Ограничение запросов к API: 10 запросов в минуту

## Структура проекта

```tree
rich/
├── main.go          # Основной код программы
├── rich.cfg         # Конфигурационный файл
├── rich.log         # Файл журнала
├── todo/            # Директория с исходными файлами
└── done/            # Директория с обработанными файлами
```

## Формат выходных файлов

Обработанные файлы сохраняются в следующем формате:

```markdown
<Обогащенное содержимое>

'''old
<Оригинальное содержимое>
'''
```

## Требования

- Go 1.15 или выше
- Доступ к API выбранной AI-модели

## Безопасность

- Используются переменные окружения для хранения API ключей
- Проверка безопасности путей (защита от path traversal)
- Валидация размера и содержимого входных файлов
- Безопасная запись файлов через временные файлы
- Строгие проверки ответов API

## Лицензия

MIT License. См. файл [LICENSE](LICENSE) для подробностей.

## Вклад в проект

1. Форкните репозиторий
2. Создайте ветку для ваших изменений
3. Внесите изменения и создайте pull request

## Контакты

Создайте issue в репозитории для сообщения о проблемах или предложений по улучшению.

## Спасибо

- [@Headcrab](https://github.com/Headcrab)
