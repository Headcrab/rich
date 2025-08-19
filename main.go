package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/ini.v1"
)

// Максимальный размер файла для обработки (10 МБ)
const MaxFileSize = 10 * 1024 * 1024

// Ограничение количества запросов в минуту
const RequestsPerMinute = 10

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
	// Если true, каждая строка входного файла рассматривается как отдельный промпт
	PerLine bool
	// Строковая спецификация выбора строк для построчного режима (например: "15,1-20,31-,-40")
	LinesSpec string
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

		// Получение API ключа из переменной окружения в зависимости от провайдера
		envKey := modelSection.Key("api_key_env").String()

		// Сначала пробуем получить ключ из файла конфигурации
		config.APIKey = modelSection.Key("api_key").String()

		// Затем, если указана переменная окружения, пробуем ее использовать
		if envKey != "" && os.Getenv(envKey) != "" {
			config.APIKey = os.Getenv(envKey)
		} else if config.APIKey == "" {
			// Автоматическое определение переменной окружения в зависимости от URL API
			apiURL := strings.ToLower(config.ModelAPIURL)
			switch {
			case strings.Contains(apiURL, "openai"):
				config.APIKey = os.Getenv("OPENAI_API_KEY")
			case strings.Contains(apiURL, "openrouter"):
				config.APIKey = os.Getenv("OPENROUTER_API_KEY")
			case strings.Contains(apiURL, "anthropic"):
				config.APIKey = os.Getenv("ANTHROPIC_API_KEY")
			}
		}

		config.Temperature = modelSection.Key("temperature").MustFloat64(0.7)
		config.MaxTokens = modelSection.Key("max_tokens").MustInt(1000)
	}

	// Чтение секции промпта
	if promptSection := cfg.Section("PROMPT"); promptSection != nil {
		config.Prompt = promptSection.Key("text").String()
	}

	// Безопасное создание выходной директории
	outputDir, err := filepath.Abs(config.OutputDir)
	if err != nil {
		return nil, fmt.Errorf("не удалось получить абсолютный путь выходной директории: %v", err)
	}

	// Проверка, что выходная директория находится в безопасном месте
	if !isPathSafe(outputDir) {
		return nil, fmt.Errorf("небезопасный путь выходной директории: %s", outputDir)
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("не удалось создать выходную директорию: %v", err)
	}

	return config, nil
}

// Проверка безопасности пути (защита от path traversal)
func isPathSafe(path string) bool {
	// Проверка на наличие подозрительных последовательностей в пути
	suspicious := []string{"../", "..\\"}
	for _, s := range suspicious {
		if strings.Contains(path, s) {
			return false
		}
	}
	return true
}

// Валидация содержимого файла
func validateContent(content []byte) error {
	// Проверка размера файла
	if len(content) > MaxFileSize {
		return fmt.Errorf("размер файла превышает максимально допустимый (%d байт)", MaxFileSize)
	}

	// Здесь можно добавить дополнительные проверки содержимого
	return nil
}

// Регулярка для строки заголовка: допускает префиксы форматирования, регистр, скобочную и обычную форму
var titleLineRe = regexp.MustCompile(`(?i)^\s*[-*#>\d\.)\(\s]*title\s*:\s*(?:\[(.*?)\]|(.+))\s*$`)

// Санитизация имени файла (для Windows/Linux): удаление недопустимых символов
func sanitizeFileName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	// Заменяем недопустимые символы
	forbidden := []string{"<", ">", ":", "\"", "/", "\\", "|", "?", "*"}
	for _, ch := range forbidden {
		name = strings.ReplaceAll(name, ch, "_")
	}
	// Убираем управляющие символы
	name = strings.Map(func(r rune) rune {
		if r < 32 {
			return -1
		}
		return r
	}, name)
	// Точки и пробелы в конце имени недопустимы в Windows
	name = strings.TrimRight(name, ". ")
	// Ограничим длину до разумной величины
	if len(name) > 120 {
		name = name[:120]
		name = strings.TrimRight(name, " .")
	}
	if name == "" {
		return "_"
	}
	return name
}

// Извлечение заголовка title:[FileTitle] из ответа и удаление строки из контента.
// Если removeNextEmpty=true, то также удаляется ПЕРВАЯ пустая строка сразу после строки заголовка.
func extractTitleAndStrip(enriched string, removeNextEmpty bool) (title string, stripped string) {
	lines := strings.Split(enriched, "\n")
	title = ""
	out := make([]string, 0, len(lines))
	skipNextIfEmpty := false

	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if skipNextIfEmpty {
			if strings.TrimSpace(l) == "" {
				// пропускаем ровно одну пустую строку после title
				skipNextIfEmpty = false
				continue
			}
			skipNextIfEmpty = false
		}

		// Пытаемся извлечь title с помощью регулярного выражения
		if m := titleLineRe.FindStringSubmatch(l); m != nil {
			candidate := ""
			if len(m) > 1 && strings.TrimSpace(m[1]) != "" {
				candidate = strings.TrimSpace(m[1])
			} else if len(m) > 2 && strings.TrimSpace(m[2]) != "" {
				candidate = strings.TrimSpace(m[2])
			}
			if candidate != "" && title == "" {
				title = sanitizeFileName(candidate)
				// Не добавляем строку title в результат
				if removeNextEmpty {
					skipNextIfEmpty = true
				}
				continue
			}
		}
		out = append(out, l)
	}

	stripped = strings.Join(out, "\n")
	return
}

// Парсинг спецификации строк (например: "15,1-20,31-,-40"). Возвращает множество выбранных строк (1-based)
func parseLineSpec(spec string, total int) map[int]bool {
	selected := make(map[int]bool)
	spec = strings.TrimSpace(spec)
	if spec == "" {
		// Пустая спецификация — ничего не ограничиваем (т.е. все строки)
		for i := 1; i <= total; i++ {
			selected[i] = true
		}
		return selected
	}

	parts := strings.Split(spec, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.Contains(p, "-") {
			// Диапазоны: a-b, a-, -b
			hy := strings.SplitN(p, "-", 2)
			left := strings.TrimSpace(hy[0])
			right := strings.TrimSpace(hy[1])
			var start, end int
			if left == "" {
				start = 1
			} else {
				if n, err := strconv.Atoi(left); err == nil {
					start = n
				} else {
					continue
				}
			}
			if right == "" {
				end = total
			} else {
				if n, err := strconv.Atoi(right); err == nil {
					end = n
				} else {
					continue
				}
			}
			if start < 1 {
				start = 1
			}
			if end > total {
				end = total
			}
			if end < start {
				continue
			}
			for i := start; i <= end; i++ {
				selected[i] = true
			}
		} else {
			// Одна строка
			if n, err := strconv.Atoi(p); err == nil {
				if n >= 1 && n <= total {
					selected[n] = true
				}
			}
		}
	}
	// Если после парсинга ничего не выбрано — выберем все, чтобы не было сюрпризов
	if len(selected) == 0 {
		for i := 1; i <= total; i++ {
			selected[i] = true
		}
	}
	return selected
}

// Ограничитель частоты запросов
type RateLimiter struct {
	tokens   chan struct{}
	interval time.Duration
}

// Создание нового ограничителя частоты запросов
func NewRateLimiter(requestsPerMinute int) *RateLimiter {
	interval := time.Minute / time.Duration(requestsPerMinute)
	limiter := &RateLimiter{
		tokens:   make(chan struct{}, requestsPerMinute),
		interval: interval,
	}

	// Инициализация токенов
	for i := 0; i < requestsPerMinute; i++ {
		limiter.tokens <- struct{}{}
	}

	// Запуск горутины для пополнения токенов
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			select {
			case limiter.tokens <- struct{}{}:
				// Токен добавлен
			default:
				// Буфер полон
			}
		}
	}()

	return limiter
}

// Ожидание доступности токена
func (r *RateLimiter) Wait() {
	<-r.tokens
}

// Обогащение markdown содержимого с использованием AI API
func enrichContent(config *Config, content string, rateLimiter *RateLimiter) (string, error) {
	// Ожидание доступности токена (ограничение частоты запросов)
	rateLimiter.Wait()

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

	// Настройка HTTP клиента с проверкой TLS сертификатов
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}
	client := &http.Client{
		Timeout:   180 * time.Second,
		Transport: transport,
	}

	// Выполнение запроса
	resp, err := client.Do(req)
	if err != nil {
		return content, fmt.Errorf("ошибка при выполнении HTTP запроса: %v", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Printf("Ошибка закрытия тела ответа: %v", cerr)
		}
	}()

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

	// Логируем только статус ответа, а не полное содержимое
	log.Printf("Получен ответ API: статус %d, размер %d байт", resp.StatusCode, len(body))

	var responseData map[string]interface{}
	if err := json.Unmarshal(body, &responseData); err != nil {
		return content, fmt.Errorf("ошибка при разборе JSON ответа: %v", err)
	}

	// Извлечение содержимого в зависимости от типа API
	var enrichedContent string
	if strings.Contains(strings.ToLower(config.ModelAPIURL), "openai") ||
		strings.Contains(strings.ToLower(config.ModelAPIURL), "openrouter") ||
		strings.Contains(strings.ToLower(config.ModelAPIURL), "chat/completions") {
		choices, ok := responseData["choices"].([]interface{})
		if !ok || len(choices) == 0 {
			return "", fmt.Errorf("некорректный формат ответа API: отсутствует поле choices или оно пустое")
		}

		firstChoice, ok := choices[0].(map[string]interface{})
		if !ok {
			return "", fmt.Errorf("некорректный формат элемента choices в ответе API")
		}

		message, ok := firstChoice["message"].(map[string]interface{})
		if !ok {
			return "", fmt.Errorf("некорректный формат поля message в ответе API")
		}

		messageContent, ok := message["content"].(string)
		if !ok {
			return "", fmt.Errorf("некорректный формат поля content в ответе API")
		}

		enrichedContent = messageContent
	} else if strings.Contains(strings.ToLower(config.ModelAPIURL), "anthropic") {
		contentArray, ok := responseData["content"].([]interface{})
		if !ok || len(contentArray) == 0 {
			return "", fmt.Errorf("некорректный формат ответа Anthropic API: отсутствует поле content или оно пустое")
		}

		firstContent, ok := contentArray[0].(map[string]interface{})
		if !ok {
			return "", fmt.Errorf("некорректный формат элемента content в ответе Anthropic API")
		}

		text, ok := firstContent["text"].(string)
		if !ok {
			return "", fmt.Errorf("некорректный формат поля text в ответе Anthropic API")
		}

		enrichedContent = text
	} else {
		text, ok := responseData["text"].(string)
		if !ok {
			return "", fmt.Errorf("некорректный формат ответа API")
		}
		enrichedContent = text
	}

	return strings.TrimSpace(enrichedContent), nil
}

// Добавление файла в список исключений
func addToExcludedFiles(configPath string, relPath string) error {
	// Нормализуем путь для кроссплатформенности
	relPath = filepath.Clean(relPath)

	cfg, err := ini.Load(configPath)
	if err != nil {
		return fmt.Errorf("не удалось загрузить файл конфигурации: %v", err)
	}

	exclSection := cfg.Section("EXCLUSIONS")
	currentExcluded := exclSection.Key("excluded_files").String()

	// Проверяем, не добавлен ли уже файл
	excludedFiles := strings.Split(currentExcluded, ",")
	for _, ef := range excludedFiles {
		// Нормализуем путь из конфига для сравнения
		existingPath := filepath.Clean(strings.TrimSpace(ef))
		if existingPath == relPath {
			return nil // Файл уже в списке
		}
	}

	// Добавляем новый файл
	if currentExcluded == "" {
		currentExcluded = relPath
	} else {
		currentExcluded += ", " + relPath
	}

	exclSection.Key("excluded_files").SetValue(currentExcluded)

	// Безопасная запись в файл конфигурации
	tempFile := configPath + ".tmp"
	if err := cfg.SaveTo(tempFile); err != nil {
		return fmt.Errorf("не удалось сохранить временный файл конфигурации: %v", err)
	}

	if err := os.Rename(tempFile, configPath); err != nil {
		return fmt.Errorf("не удалось переименовать временный файл конфигурации: %v", err)
	}

	return nil
}

// Безопасная запись в файл
func safeWriteFile(path string, data []byte, perm os.FileMode) error {
	// Создание временного файла
	dir := filepath.Dir(path)
	tempFile, err := os.CreateTemp(dir, "temp_*.tmp")
	if err != nil {
		return fmt.Errorf("не удалось создать временный файл: %v", err)
	}
	tempPath := tempFile.Name()

	// Запись данных во временный файл
	if _, err := tempFile.Write(data); err != nil {
		if cerr := tempFile.Close(); cerr != nil {
			log.Printf("Ошибка закрытия временного файла: %v", cerr)
		}
		if rerr := os.Remove(tempPath); rerr != nil {
			log.Printf("Ошибка удаления временного файла: %v", rerr)
		}
		return fmt.Errorf("не удалось записать данные во временный файл: %v", err)
	}

	if err := tempFile.Close(); err != nil {
		if rerr := os.Remove(tempPath); rerr != nil {
			log.Printf("Ошибка удаления временного файла: %v", rerr)
		}
		return fmt.Errorf("не удалось закрыть временный файл: %v", err)
	}

	// Установка прав доступа
	if err := os.Chmod(tempPath, perm); err != nil {
		if rerr := os.Remove(tempPath); rerr != nil {
			log.Printf("Ошибка удаления временного файла: %v", rerr)
		}
		return fmt.Errorf("не удалось установить права доступа для временного файла: %v", err)
	}

	// Переименование временного файла в целевой
	if err := os.Rename(tempPath, path); err != nil {
		if rerr := os.Remove(tempPath); rerr != nil {
			log.Printf("Ошибка удаления временного файла: %v", rerr)
		}
		return fmt.Errorf("не удалось переименовать временный файл: %v", err)
	}

	return nil
}

// Обработка одного markdown файла
func processFile(config *Config, inputPath, outputPath string, configPath string, rateLimiter *RateLimiter) error {
	log.Printf("Обработка %s", inputPath)

	// Проверка безопасности путей
	if !isPathSafe(inputPath) || !isPathSafe(outputPath) {
		return fmt.Errorf("обнаружен небезопасный путь: %s или %s", inputPath, outputPath)
	}

	// Чтение оригинального содержимого
	content, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("ошибка при чтении файла: %v", err)
	}

	// Валидация содержимого файла
	if err := validateContent(content); err != nil {
		return fmt.Errorf("ошибка валидации содержимого файла: %v", err)
	}

	// Обогащение содержимого
	enrichedContent, err := enrichContent(config, string(content), rateLimiter)
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

	// Извлекаем потенциальный заголовок и удаляем строку title: из контента
	title, stripped := extractTitleAndStrip(enrichedContent, false)
	if strings.TrimSpace(stripped) == "" {
		stripped = enrichedContent
	}

	// Объединение обогащенного содержимого с оригинальным в указанном формате
	finalContent := fmt.Sprintf("%s\n\n```old\n%s\n```", stripped, escapedContent)

	// Если в ответе был title — используем его как имя файла
	if title != "" {
		dir := filepath.Dir(outputPath)
		ext := filepath.Ext(outputPath)
		if ext == "" {
			ext = ".md"
		}
		outputPath = filepath.Join(dir, title+ext)
	}

	// Безопасная запись результата
	if err := safeWriteFile(outputPath, []byte(finalContent), 0644); err != nil {
		return fmt.Errorf("ошибка при записи выходного файла: %v", err)
	}

	// Добавляем обработанный файл в список исключений только при успешном обогащении
	relPath, errRel := filepath.Rel(config.InputDir, inputPath)
	if errRel != nil || strings.Contains(relPath, "..") {
		relPath = filepath.Base(inputPath)
	}
	if err := addToExcludedFiles(configPath, relPath); err != nil {
		// Обрабатываем ошибку, но не прерываем выполнение
		log.Printf("Предупреждение: не удалось добавить файл в список исключений: %v", err)
		// Попытка повторить операцию
		if retryErr := addToExcludedFiles(configPath, relPath); retryErr != nil {
			log.Printf("Ошибка при повторной попытке добавить файл в список исключений: %v", retryErr)
		}
	}

	log.Printf("Сохранено обогащенное содержимое в %s", outputPath)
	return nil
}

// Построчная обработка markdown-файла: каждая непустая строка как отдельный промпт
func processFilePerLine(config *Config, inputPath, outputPath string, configPath string, rateLimiter *RateLimiter) error {
	log.Printf("Построчная обработка %s", inputPath)

	// Безопасность путей
	if !isPathSafe(inputPath) || !isPathSafe(outputPath) {
		return fmt.Errorf("обнаружен небезопасный путь: %s или %s", inputPath, outputPath)
	}

	// Чтение оригинального содержимого
	content, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("ошибка при чтении файла: %v", err)
	}

	// Валидация содержимого файла
	if err := validateContent(content); err != nil {
		return fmt.Errorf("ошибка валидации содержимого файла: %v", err)
	}

	// Подготовка директории для выходных файлов
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("ошибка при создании выходной директории: %v", err)
	}

	// Базовые части имени
	base := strings.TrimSuffix(filepath.Base(outputPath), filepath.Ext(outputPath))
	ext := filepath.Ext(outputPath)
	if ext == "" {
		ext = ".md"
	}

	// Разбиваем содержимое по строкам
	rawLines := strings.Split(string(content), "\n")
	// Уберём завершающие \r
	for i := range rawLines {
		rawLines[i] = strings.TrimRight(rawLines[i], "\r")
	}

	totalLines := len(rawLines)
	if totalLines == 0 {
		log.Printf("Файл %s не содержит непустых строк — пропуск", inputPath)
		return nil
	}

	// Определяем, какие строки обрабатывать
	selected := parseLineSpec(config.LinesSpec, totalLines)
	width := len(strconv.Itoa(totalLines))
	success := 0
	for idx, ln := range rawLines {
		lineNo := idx + 1 // 1-based
		if !selected[lineNo] {
			continue
		}
		if strings.TrimSpace(ln) == "" {
			// Пустые строки не обрабатываем
			continue
		}
		// Обогащение одной строки
		enriched, err := enrichContent(config, ln, rateLimiter)
		if err != nil {
			log.Printf("Предупреждение: ошибка при обогащении строки %d в %s: %v", lineNo, inputPath, err)
			continue
		}

		// Извлекаем возможный заголовок и очищаем контент
		title, stripped := extractTitleAndStrip(enriched, true)
		if strings.TrimSpace(stripped) == "" {
			stripped = enriched
		}

		// Экранирование обратных кавычек в оригинальной строке
		escaped := strings.ReplaceAll(ln, "```", "\\`\\`\\`")
		finalContent := fmt.Sprintf("%s\n\n```old\n%s\n```", stripped, escaped)

		// Имя файла: <basename|title>__line-<NNN><ext>
		effectiveBase := base
		if title != "" {
			effectiveBase = title
		}
		lineName := fmt.Sprintf("%s__line-%0*d%s", effectiveBase, width, lineNo, ext)
		lineOutputPath := filepath.Join(outputDir, lineName)

		if err := safeWriteFile(lineOutputPath, []byte(finalContent), 0644); err != nil {
			log.Printf("Предупреждение: не удалось записать результат для строки %d (%s): %v", lineNo, lineOutputPath, err)
			continue
		}

		success++
		log.Printf("Сохранено обогащенное содержимое строки %d в %s", lineNo, lineOutputPath)
	}

	// Добавляем исходный файл в исключения, если что-то удалось обработать
	if success > 0 {
		relPath, errRel := filepath.Rel(config.InputDir, inputPath)
		if errRel != nil || strings.Contains(relPath, "..") {
			relPath = filepath.Base(inputPath)
		}
		if err := addToExcludedFiles(configPath, filepath.Clean(relPath)); err != nil {
			log.Printf("Предупреждение: не удалось добавить файл в список исключений: %v", err)
			if retryErr := addToExcludedFiles(configPath, filepath.Clean(relPath)); retryErr != nil {
				log.Printf("Ошибка при повторной попытке добавить файл в список исключений: %v", retryErr)
			}
		}
		return nil
	}

	return fmt.Errorf("не удалось обработать ни одной строки файла %s", inputPath)
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

	// Проверка безопасности путей
	if !isPathSafe(inputDir) || !isPathSafe(outputDir) {
		return fmt.Errorf("обнаружен небезопасный путь директории: %s или %s", inputDir, outputDir)
	}

	// Множество исключенных файлов для быстрого поиска
	excludedMap := make(map[string]bool)
	for _, file := range config.ExcludedFiles {
		cleaned := filepath.Clean(file)
		excludedMap[cleaned] = true
	}

	// Создание ограничителя частоты запросов
	rateLimiter := NewRateLimiter(RequestsPerMinute)

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

		// Определение пути относительно входной директории
		relPath, err := filepath.Rel(inputDir, path)
		if err != nil {
			return fmt.Errorf("ошибка при получении относительного пути: %v", err)
		}

		// Проверка на path traversal
		if strings.Contains(relPath, "..") {
			return fmt.Errorf("обнаружена попытка path traversal: %s", relPath)
		}

		// Проверка на исключенные файлы по относительному пути
		relPath = filepath.Clean(relPath)
		if excludedMap[relPath] {
			log.Printf("Пропуск исключенного файла: %s", relPath)
			return nil
		}

		// Определение пути выходного файла (для стандартного режима это полный путь; для построчного — директория и базовое имя будут использованы)
		outputPath := filepath.Join(outputDir, relPath)

		// Обработка файла (обычная или построчная)
		var perr error
		if config.PerLine {
			perr = processFilePerLine(config, path, outputPath, configPath, rateLimiter)
		} else {
			perr = processFile(config, path, outputPath, configPath, rateLimiter)
		}
		if perr != nil {
			log.Printf("Ошибка при обработке %s: %v", path, perr)
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
	perLine := flag.Bool("per-line", false, "Рассматривать каждую строку файла как отдельный промпт и сохранять результат в отдельном файле")
	linesSpec := flag.String("lines", "", "Какие строки обрабатывать (напр.: '15,1-20,31-,-40'). Поддерживает одиночные номера и диапазоны")
	flag.Parse()

	// Настройка логирования
	logFile, err := os.OpenFile("rich.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Не удалось открыть файл журнала: %v", err)
	}
	defer func() {
		if cerr := logFile.Close(); cerr != nil {
			log.Printf("Ошибка закрытия файла журнала: %v", cerr)
		}
	}()

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.SetOutput(io.MultiWriter(os.Stdout, logFile))

	log.Printf("Запуск с конфигурацией из: %s", *configPath)

	// Загрузка конфигурации
	config, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("Ошибка загрузки конфигурации: %v", err)
	}

	// Применяем флаг построчной обработки
	config.PerLine = *perLine
	config.LinesSpec = strings.TrimSpace(*linesSpec)

	// Обработка директории
	if err := processDirectory(config, *configPath); err != nil {
		log.Fatalf("Ошибка обработки директории: %v", err)
	}

	log.Println("Обработка завершена")
}
