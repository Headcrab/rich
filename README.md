# Rich - Инструмент обогащения markdown-контента с помощью AI

[![Go Version](https://img.shields.io/github/go-mod/go-version/Headcrab/rich)](https://go.dev)
[![License](https://img.shields.io/github/license/Headcrab/rich)](LICENSE)
[![Coverage](https://codecov.io/gh/Headcrab/rich/graph/badge.svg)](https://codecov.io/gh/Headcrab/rich)

Rich - это утилита командной строки, разработанная на Go, которая автоматически обогащает markdown-файлы с использованием различных моделей искусственного интеллекта (GPT-4, Claude, Gemini). Инструмент анализирует содержимое markdown-файлов и дополняет их детальной информацией, ссылками и пояснениями.

## Возможности

- 🤖 Поддержка различных AI-моделей (OpenAI GPT-4, Anthropic Claude, Google Gemini)
- 📁 Пакетная обработка markdown-файлов
- ⚙️ Гибкая настройка через конфигурационный файл
- 🔄 Сохранение оригинальной структуры документов
- 🎯 Настраиваемые промпты для точной генерации контента
- 🚫 Возможность исключения файлов из обработки

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

## Конфигурация

Создайте файл `rich.cfg` в директории проекта:

```ini
[DIRECTORIES]
input_dir  = ./todo    # Директория с исходными файлами
output_dir = ./done    # Директория для обработанных файлов

[EXCLUSIONS]
excluded_files = README.md, CHANGELOG.md, LICENSE.md

[MODEL]
name        = google/gemini-2.0-pro-exp-02-05:free
api_url     = https://openrouter.ai/api/v1/chat/completions
api_key     = your-api-key
temperature = 0.7
max_tokens  = 32000

[PROMPT]
text = """Ваш промпт для обогащения контента"""
```

## Использование

1. Подготовьте markdown-файлы в директории `input_dir`
2. Настройте параметры в `rich.cfg`
3. Запустите обработку:

```bash
./rich -config rich.cfg
```

### Параметры командной строки

- `-config` - путь к конфигурационному файлу (по умолчанию: rich.cfg)
- `-v` - включить подробный вывод

## Структура проекта

```tree
rich/
├── main.go          # Основной код программы
├── rich.cfg         # Конфигурационный файл
├── todo/         # Директория с исходными файлами
└── done/        # Директория с обработанными файлами
```

## Требования

- Go 1.21 или выше
- Доступ к API выбранной AI-модели

## Лицензия

MIT License. См. файл [LICENSE](LICENSE) для подробностей.

## Вклад в проект

1. Форкните репозиторий
2. Создайте ветку для ваших изменений
3. Внесите изменения и создайте pull request

## Безопасность

- Храните API-ключи в безопасном месте
- Не включайте конфиденциальную информацию в обрабатываемые файлы
- Проверяйте содержимое обработанных файлов перед использованием

## Контакты

Создайте issue в репозитории для сообщения о проблемах или предложений по улучшению.

## Спасибо

- [@Headcrab](https://github.com/Headcrab)
