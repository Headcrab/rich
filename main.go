package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/ini.v1"
)

// Конфигурация для процесса обогащения
type Config struct {
	InputDir      string
	OutputDir     string
	ExcludedFiles []string
	ModelName     string
	ModelAPIURL   string
	APIKey        string
	Prompt        string
	Temperature   float64
	MaxTokens     int
}

// Загрузка конфигурации из INI файла
func loadConfig(configPath string) (*Config, error) {
	// Проверка наличия файла конфигурации
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("файл конфигурации не найден: %s", configPath)
	}

	// Загрузка INI файла
	cfg, err := ini.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("не удалось загрузить файл конфигурации: %v", err)
	}

	// Инициализация конфигурации с настройками по умолчанию
	config := &Config{
		InputDir:    "./todo",
		OutputDir:   "./done",
		Temperature: 0.7,
		MaxTokens:   1000,
	}

	// Чтение секции директорий
	if dirSection := cfg.Section("DIRECTORIES"); dirSection != nil {
		config.InputDir = dirSection.Key("input_dir").MustString("./todo")
		config.OutputDir = dirSection.Key("output_dir").MustString("./done")
	}

	// Чтение секции исключений
	if exclSection := cfg.Section("EXCLUSIONS"); exclSection != nil {
		excludedStr := exclSection.Key("excluded_files").String()
		if excludedStr != "" {
			excluded := strings.Split(excludedStr, ",")
			config.ExcludedFiles = make([]string, 0, len(excluded))
			for _, file := range excluded {
				trimmed := strings.TrimSpace(file)
				if trimmed != "" {
					config.ExcludedFiles = append(config.ExcludedFiles, trimmed)
				}
			}
		}
	}

	// Чтение конфигурации модели
	if modelSection := cfg.Section("MODEL"); modelSection != nil {
		config.ModelName = modelSection.Key("name").MustString("gpt-3.5-turbo")
		config.ModelAPIURL = modelSection.Key("api_url").MustString("https://api.openai.com/v1/chat/completions")
		config.APIKey = modelSection.Key("api_key").String()
		config.Temperature = modelSection.Key("temperature").MustFloat64(0.7)
		config.MaxTokens = modelSection.Key("max_tokens").MustInt(1000)
	}

	// Чтение секции промпта
	if promptSection := cfg.Section("PROMPT"); promptSection != nil {
		config.Prompt = promptSection.Key("text").String()
	}

	// Создать выходную директорию, если она не существует
	if err := os.MkdirAll(config.OutputDir, 0755); err != nil {
		return nil, fmt.Errorf("не удалось создать выходную директорию: %v", err)
	}

	return config, nil
}

// Обогащение markdown содержимого с использованием AI API
func enrichContent(config *Config, content string) (string, error) {
	// Подготовка полного промпта с содержимым
	fullPrompt := fmt.Sprintf("%s\n\n%s", config.Prompt, content)

	// Подготовка запроса на основе типа API
	var requestBody []byte
	var err error

	if strings.Contains(strings.ToLower(config.ModelAPIURL), "openai") || strings.Contains(strings.ToLower(config.ModelAPIURL), "openrouter") {
		// Формат запроса OpenAI/OpenRouter
		requestData := map[string]interface{}{
			"model": config.ModelName,
			"messages": []map[string]string{
				{"role": "user", "content": fullPrompt},
			},
			"temperature": config.Temperature,
			"max_tokens":  config.MaxTokens,
		}
		requestBody, err = json.Marshal(requestData)
	} else if strings.Contains(strings.ToLower(config.ModelAPIURL), "anthropic") {
		// Формат запроса Anthropic
		requestData := map[string]interface{}{
			"model": config.ModelName,
			"messages": []map[string]string{
				{"role": "user", "content": fullPrompt},
			},
			"temperature": config.Temperature,
			"max_tokens":  config.MaxTokens,
		}
		requestBody, err = json.Marshal(requestData)
	} else {
		// Общий формат API
		requestData := map[string]interface{}{
			"model":       config.ModelName,
			"prompt":      fullPrompt,
			"temperature": config.Temperature,
			"max_tokens":  config.MaxTokens,
		}
		requestBody, err = json.Marshal(requestData)
	}

	if err != nil {
		return content, fmt.Errorf("ошибка при подготовке JSON запроса: %v", err)
	}

	// Формирование URL в зависимости от API
	apiURL := config.ModelAPIURL
	if strings.Contains(strings.ToLower(config.ModelAPIURL), "anthropic") {
		apiURL = "https://api.anthropic.com/v1/messages"
	}

	// Создание HTTP запроса
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(requestBody))
	if err != nil {
		return content, fmt.Errorf("ошибка при создании HTTP запроса: %v", err)
	}

	// Установка заголовков
	req.Header.Set("Content-Type", "application/json")
	if config.APIKey != "" {
		if strings.Contains(strings.ToLower(config.ModelAPIURL), "anthropic") {
			req.Header.Set("x-api-key", config.APIKey)
			req.Header.Set("anthropic-version", "2023-06-01")
		} else if strings.Contains(strings.ToLower(config.ModelAPIURL), "openrouter") {
			req.Header.Set("Authorization", "Bearer "+config.APIKey)
			req.Header.Set("HTTP-Referer", "https://github.com/")
			req.Header.Set("X-Title", "Markdown Enricher")
		} else {
			req.Header.Set("Authorization", "Bearer "+config.APIKey)
		}
	}

	// Выполнение запроса
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return content, fmt.Errorf("ошибка при выполнении HTTP запроса: %v", err)
	}
	defer resp.Body.Close()

	// Проверка статуса ответа
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return content, fmt.Errorf("API запрос вернул статус %d: %s", resp.StatusCode, string(body))
	}

	// Чтение и парсинг ответа
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return content, fmt.Errorf("ошибка при чтении ответа API: %v", err)
	}

	// Логируем ответ для отладки
	log.Printf("Ответ API: %s", string(body))

	var responseData map[string]interface{}
	if err := json.Unmarshal(body, &responseData); err != nil {
		return content, fmt.Errorf("ошибка при разборе JSON ответа: %v", err)
	}

	// Извлечение содержимого в зависимости от типа API
	var enrichedContent string
	if strings.Contains(strings.ToLower(config.ModelAPIURL), "openai") || strings.Contains(strings.ToLower(config.ModelAPIURL), "openrouter") {
		choices, ok := responseData["choices"].([]interface{})
		if !ok || len(choices) == 0 {
			return "", fmt.Errorf("некорректный формат ответа API: отсутствует поле choices или оно пустое: %s", string(body))
		}

		firstChoice, ok := choices[0].(map[string]interface{})
		if !ok {
			return "", fmt.Errorf("некорректный формат элемента choices в ответе API: %s", string(body))
		}

		message, ok := firstChoice["message"].(map[string]interface{})
		if !ok {
			return "", fmt.Errorf("некорректный формат поля message в ответе API: %s", string(body))
		}

		messageContent, ok := message["content"].(string)
		if !ok {
			return "", fmt.Errorf("некорректный формат поля content в ответе API: %s", string(body))
		}

		enrichedContent = messageContent
	} else if strings.Contains(strings.ToLower(config.ModelAPIURL), "anthropic") {
		contentArray, ok := responseData["content"].([]interface{})
		if !ok || len(contentArray) == 0 {
			return "", fmt.Errorf("некорректный формат ответа Anthropic API: отсутствует поле content или оно пустое: %s", string(body))
		}

		firstContent, ok := contentArray[0].(map[string]interface{})
		if !ok {
			return "", fmt.Errorf("некорректный формат элемента content в ответе Anthropic API: %s", string(body))
		}

		text, ok := firstContent["text"].(string)
		if !ok {
			return "", fmt.Errorf("некорректный формат поля text в ответе Anthropic API: %s", string(body))
		}

		enrichedContent = text
	} else {
		text, ok := responseData["text"].(string)
		if !ok {
			return "", fmt.Errorf("некорректный формат ответа API: %s", string(body))
		}
		enrichedContent = text
	}

	return strings.TrimSpace(enrichedContent), nil
}

// Добавление файла в список исключений
func addToExcludedFiles(configPath string, filename string) error {
	cfg, err := ini.Load(configPath)
	if err != nil {
		return fmt.Errorf("не удалось загрузить файл конфигурации: %v", err)
	}

	exclSection := cfg.Section("EXCLUSIONS")
	currentExcluded := exclSection.Key("excluded_files").String()

	// Проверяем, не добавлен ли уже файл
	excludedFiles := strings.Split(currentExcluded, ",")
	for _, ef := range excludedFiles {
		if strings.TrimSpace(ef) == filename {
			return nil // Файл уже в списке
		}
	}

	// Добавляем новый файл
	if currentExcluded == "" {
		currentExcluded = filename
	} else {
		currentExcluded += ", " + filename
	}

	exclSection.Key("excluded_files").SetValue(currentExcluded)

	return cfg.SaveTo(configPath)
}

// Обработка одного markdown файла
func processFile(config *Config, inputPath, outputPath string, configPath string) error {
	log.Printf("Обработка %s", inputPath)

	// Чтение оригинального содержимого
	content, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("ошибка при чтении файла: %v", err)
	}

	// Обогащение содержимого
	enrichedContent, err := enrichContent(config, string(content))
	if err != nil {
		log.Printf("Предупреждение: ошибка при обогащении содержимого %s: %v", inputPath, err)
		return err // Возвращаем ошибку и прекращаем обработку файла
	}

	// Подготовка директории для выходного файла
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("ошибка при создании выходной директории: %v", err)
	}

	// Экранирование тройных обратных кавычек в оригинальном содержимом
	escapedContent := strings.ReplaceAll(string(content), "```", "\\`\\`\\`")

	// Объединение обогащенного содержимого с оригинальным в указанном формате
	finalContent := fmt.Sprintf("%s\n\n```old\n%s\n```", enrichedContent, escapedContent)

	// Запись результата
	if err := os.WriteFile(outputPath, []byte(finalContent), 0644); err != nil {
		return fmt.Errorf("ошибка при записи выходного файла: %v", err)
	}

	// Добавляем обработанный файл в список исключений только при успешном обогащении
	filename := filepath.Base(inputPath)
	if err := addToExcludedFiles(configPath, filename); err != nil {
		log.Printf("Предупреждение: не удалось добавить файл в список исключений: %v", err)
	}

	log.Printf("Сохранено обогащенное содержимое в %s", outputPath)
	return nil
}

// Обработка директории для получения всех markdown файлов
func processDirectory(config *Config, configPath string) error {
	// Преобразование путей в абсолютные
	inputDir, err := filepath.Abs(config.InputDir)
	if err != nil {
		return fmt.Errorf("ошибка при получении абсолютного пути входной директории: %v", err)
	}

	outputDir, err := filepath.Abs(config.OutputDir)
	if err != nil {
		return fmt.Errorf("ошибка при получении абсолютного пути выходной директории: %v", err)
	}

	// Множество исключенных файлов для быстрого поиска
	excludedMap := make(map[string]bool)
	for _, file := range config.ExcludedFiles {
		excludedMap[file] = true
	}

	// Счетчик обработанных файлов
	fileCount := 0

	// Обход всех .md файлов в директории и поддиректориях
	err = filepath.Walk(inputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Пропуск директорий
		if info.IsDir() {
			return nil
		}

		// Проверка расширения файла
		if !strings.HasSuffix(strings.ToLower(info.Name()), ".md") {
			return nil
		}

		// Проверка на исключенные файлы
		if excludedMap[info.Name()] {
			log.Printf("Пропуск исключенного файла: %s", info.Name())
			return nil
		}

		// Определение пути выходного файла
		relPath, err := filepath.Rel(inputDir, path)
		if err != nil {
			return fmt.Errorf("ошибка при получении относительного пути: %v", err)
		}

		outputPath := filepath.Join(outputDir, relPath)

		// Обработка файла
		if err := processFile(config, path, outputPath, configPath); err != nil {
			log.Printf("Ошибка при обработке %s: %v", path, err)
			return nil // Продолжаем с другими файлами
		}

		fileCount++
		return nil
	})

	if err != nil {
		return fmt.Errorf("ошибка при обходе директории: %v", err)
	}

	log.Printf("Обработано файлов: %d", fileCount)
	return nil
}

func main() {
	// Обработка аргументов командной строки
	configPath := flag.String("config", "rich.cfg", "Путь к файлу конфигурации")
	flag.Parse()

	// Настройка логирования
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.SetOutput(io.MultiWriter(os.Stdout))

	log.Printf("Запуск с конфигурацией из: %s", *configPath)

	// Загрузка конфигурации
	config, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("Ошибка загрузки конфигурации: %v", err)
	}

	// Обработка директории
	if err := processDirectory(config, *configPath); err != nil {
		log.Fatalf("Ошибка обработки директории: %v", err)
	}

	log.Println("Обработка завершена")
}
