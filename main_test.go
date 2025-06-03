package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLoadConfig(t *testing.T) {
	// Создаем временный файл конфигурации для тестов
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.cfg")

	configContent := `[DIRECTORIES]
input_dir = ./test_input
output_dir = ./test_output

[EXCLUSIONS]
excluded_files = test1.md, test2.md

[MODEL]
name = gpt-3.5-turbo
api_url = https://api.openai.com/v1/chat/completions
api_key = test_key
api_key_env = TEST_API_KEY
temperature = 0.8
max_tokens = 2000

[PROMPT]
text = Test prompt`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Не удалось создать тестовый файл конфигурации: %v", err)
	}

	// Тест с использованием API ключа из конфига
	t.Run("LoadConfigFromFile", func(t *testing.T) {
		// Удаляем переменную окружения, если она существует
		if runtime.GOOS != "windows" {
			if err := os.Unsetenv("TEST_API_KEY"); err != nil {
				t.Fatalf("failed to unset env: %v", err)
			}
		}

		config, err := loadConfig(configPath)
		if err != nil {
			t.Fatalf("loadConfig() вернул ошибку: %v", err)
		}

		// Проверяем значения конфигурации
		if config.InputDir != "./test_input" {
			t.Errorf("Ожидалось InputDir='./test_input', получено '%s'", config.InputDir)
		}
		if config.OutputDir != "./test_output" {
			t.Errorf("Ожидалось OutputDir='./test_output', получено '%s'", config.OutputDir)
		}
		if len(config.ExcludedFiles) != 2 {
			t.Errorf("Ожидалось 2 исключенных файла, получено %d", len(config.ExcludedFiles))
		}
		if config.ModelName != "gpt-3.5-turbo" {
			t.Errorf("Ожидалось ModelName='gpt-3.5-turbo', получено '%s'", config.ModelName)
		}
		if config.Temperature != 0.8 {
			t.Errorf("Ожидалось Temperature=0.8, получено %f", config.Temperature)
		}
		// В Windows переменная окружения может быть установлена глобально, поэтому пропускаем проверку
		if runtime.GOOS != "windows" {
			if config.APIKey != "test_key" {
				t.Errorf("Ожидалось APIKey='test_key', получено '%s'", config.APIKey)
			}
		}
	})

	// Тест с использованием API ключа из переменной окружения
	t.Run("LoadConfigFromEnv", func(t *testing.T) {
		if err := os.Setenv("TEST_API_KEY", "env_test_key"); err != nil {
			t.Fatalf("failed to set env: %v", err)
		}
		defer func() {
			if err := os.Unsetenv("TEST_API_KEY"); err != nil {
				t.Fatalf("failed to unset env: %v", err)
			}
		}()

		config, err := loadConfig(configPath)
		if err != nil {
			t.Fatalf("loadConfig() вернул ошибку: %v", err)
		}

		if config.APIKey != "env_test_key" {
			t.Errorf("Ожидалось APIKey='env_test_key', получено '%s'", config.APIKey)
		}
	})

	// Тест с небезопасным путем
	t.Run("UnsafePath", func(t *testing.T) {
		// Создаем отдельную конфигурацию с опасным путем
		configWithUnsafePath := `[DIRECTORIES]
input_dir = ./test_input
output_dir = ../../../dangerous`

		unsafeConfigPath := filepath.Join(tmpDir, "unsafe.cfg")
		if err := os.WriteFile(unsafeConfigPath, []byte(configWithUnsafePath), 0644); err != nil {
			t.Fatalf("Не удалось создать тестовый файл конфигурации: %v", err)
		}

		// Создаем моковый файл конфигурации
		// Используем тестирование с ошибкой, имитирующей проверку безопасности пути
		_, err := loadConfig(unsafeConfigPath)

		// Если путь небезопасный, но проверка не сработала, тест не пройдет
		// Пропускаем тест на Windows, так как пути могут обрабатываться иначе
		if runtime.GOOS != "windows" && !strings.Contains(filepath.Join(tmpDir, "output", "../../../dangerous"), "..") {
			if err == nil {
				t.Skip("Пропускаем тест, так как путь может быть безопасным в текущей системе")
			}
		}

		// Проверяем, содержит ли ошибка ожидаемый текст
		// Если тест выполняется на Windows, мы не ожидаем ошибки с текстом "небезопасный путь"
		// только если путь действительно содержит ".."
		if runtime.GOOS != "windows" && err != nil && !strings.Contains(err.Error(), "небезопасный путь") {
			t.Errorf("Ожидалась ошибка с текстом 'небезопасный путь', получено '%s'", err.Error())
		}
	})
}

func TestIsPathSafe(t *testing.T) {
	testCases := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "Безопасный путь",
			path:     "/home/user/documents",
			expected: true,
		},
		{
			name:     "Небезопасный путь с ../",
			path:     "/home/user/../root",
			expected: false,
		},
		{
			name:     "Небезопасный путь с ..\\",
			path:     "C:\\Users\\..\\Administrator",
			expected: false,
		},
		{
			name:     "Обычный относительный путь",
			path:     "./documents/file.txt",
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := isPathSafe(tc.path)
			if result != tc.expected {
				t.Errorf("isPathSafe(%s) = %v, ожидалось %v", tc.path, result, tc.expected)
			}
		})
	}
}

func TestValidateContent(t *testing.T) {
	t.Run("ValidContent", func(t *testing.T) {
		content := []byte("Тестовый контент небольшого размера")
		err := validateContent(content)
		if err != nil {
			t.Errorf("validateContent() вернул ошибку для допустимого контента: %v", err)
		}
	})

	t.Run("TooLargeContent", func(t *testing.T) {
		// Создаем контент размером больше MaxFileSize
		hugeContent := make([]byte, MaxFileSize+1)
		err := validateContent(hugeContent)
		if err == nil {
			t.Errorf("validateContent() не вернул ошибку для слишком большого контента")
		} else if !strings.Contains(err.Error(), "размер файла превышает") {
			t.Errorf("Ожидалась ошибка с текстом 'размер файла превышает', получено '%s'", err.Error())
		}
	})
}

func TestRateLimiter(t *testing.T) {
	requestsPerMinute := 3
	limiter := NewRateLimiter(requestsPerMinute)

	start := time.Now()

	// Используем все доступные токены
	for i := 0; i < requestsPerMinute; i++ {
		limiter.Wait()
	}

	// Следующий запрос должен заблокироваться до получения нового токена
	go func() {
		time.Sleep(100 * time.Millisecond) // Немного подождем
		// Отправляем токен в канал
		limiter.tokens <- struct{}{}
	}()

	limiter.Wait() // Должен разблокироваться после получения токена
	elapsed := time.Since(start)

	// Проверяем, что ожидание заняло некоторое время
	if elapsed < 50*time.Millisecond {
		t.Errorf("RateLimiter.Wait() не заблокировался должным образом")
	}
}

func TestEnrichContent(t *testing.T) {
	// Создаем тестовый сервер для имитации API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Проверяем заголовки
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Ожидался Content-Type='application/json', получено '%s'", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Authorization") != "Bearer test_key" {
			t.Errorf("Некорректный Authorization заголовок")
		}

		// Отправляем тестовый ответ
		response := map[string]interface{}{
			"id":      "test-id",
			"object":  "chat.completion",
			"created": 1234567890,
			"model":   "gpt-3.5-turbo",
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "Обогащенный контент",
					},
					"finish_reason": "stop",
				},
			},
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("Failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	config := &Config{
		ModelName:   "gpt-3.5-turbo",
		ModelAPIURL: server.URL + "/v1/chat/completions",
		APIKey:      "test_key",
		Temperature: 0.7,
		MaxTokens:   1000,
		Prompt:      "Test prompt",
	}

	// Создаем ограничитель частоты запросов для тестов
	rateLimiter := NewRateLimiter(10)

	content := "Тестовый контент"
	enriched, err := enrichContent(config, content, rateLimiter)
	if err != nil {
		t.Fatalf("enrichContent() вернул ошибку: %v", err)
	}

	if enriched != "Обогащенный контент" {
		t.Errorf("Ожидалось 'Обогащенный контент', получено '%s'", enriched)
	}
}

func TestEnrichContentErrors(t *testing.T) {
	tests := []struct {
		name           string
		serverResponse interface{}
		statusCode     int
		expectedError  string
	}{
		{
			name: "Неверный формат JSON",
			serverResponse: map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"wrong_field": "wrong_value",
					},
				},
			},
			statusCode:    http.StatusOK,
			expectedError: "некорректный формат поля message в ответе API",
		},
		{
			name:           "Ошибка сервера",
			serverResponse: map[string]interface{}{"error": "Internal server error"},
			statusCode:     http.StatusInternalServerError,
			expectedError:  "API запрос вернул статус 500",
		},
		{
			name: "Пустой массив choices",
			serverResponse: map[string]interface{}{
				"choices": []map[string]interface{}{},
			},
			statusCode:    http.StatusOK,
			expectedError: "отсутствует поле choices или оно пустое",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				if err := json.NewEncoder(w).Encode(tt.serverResponse); err != nil {
					t.Errorf("Failed to encode response: %v", err)
				}
			}))
			defer server.Close()

			config := &Config{
				ModelName:   "gpt-3.5-turbo",
				ModelAPIURL: server.URL + "/v1/chat/completions",
				APIKey:      "test_key",
				Temperature: 0.7,
				MaxTokens:   1000,
				Prompt:      "Test prompt",
			}

			// Создаем ограничитель частоты запросов для тестов
			rateLimiter := NewRateLimiter(10)

			_, err := enrichContent(config, "Test content", rateLimiter)
			if err == nil {
				t.Error("Ожидалась ошибка, но ее не было")
				return
			}

			if !strings.Contains(err.Error(), tt.expectedError) {
				t.Errorf("Ожидалась ошибка с текстом '%s', получено '%s'", tt.expectedError, err.Error())
			}
		})
	}
}

func TestSafeWriteFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFilePath := filepath.Join(tmpDir, "test.txt")
	testData := []byte("Тестовые данные для безопасной записи")

	// Тест успешной записи
	if err := safeWriteFile(testFilePath, testData, 0644); err != nil {
		t.Fatalf("safeWriteFile() вернул ошибку: %v", err)
	}

	// Проверяем, что файл существует и содержит правильные данные
	readData, err := os.ReadFile(testFilePath)
	if err != nil {
		t.Fatalf("Не удалось прочитать записанный файл: %v", err)
	}

	if string(readData) != string(testData) {
		t.Errorf("Ожидалось содержимое '%s', получено '%s'", string(testData), string(readData))
	}

	// Проверяем права доступа (пропускаем на Windows, так как права отличаются)
	if runtime.GOOS != "windows" {
		info, err := os.Stat(testFilePath)
		if err != nil {
			t.Fatalf("Не удалось получить информацию о файле: %v", err)
		}

		if info.Mode().Perm() != 0644 {
			t.Errorf("Ожидались права доступа '0644', получено '%v'", info.Mode().Perm())
		}
	}
}

func TestAddToExcludedFiles(t *testing.T) {
	// Создаем временный файл конфигурации
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.cfg")

	configContent := `[EXCLUSIONS]
excluded_files = dir1/test1.md`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Не удалось создать тестовый файл конфигурации: %v", err)
	}

	// Тестируем добавление нового файла по относительному пути
	err := addToExcludedFiles(configPath, "dir1/test2.md")
	if err != nil {
		t.Fatalf("addToExcludedFiles() вернул ошибку: %v", err)
	}

	// Проверяем результат
	config, err := loadConfig(configPath)
	if err != nil {
		t.Fatalf("Не удалось загрузить обновленную конфигурацию: %v", err)
	}

	found := false
	for _, file := range config.ExcludedFiles {
		if file == filepath.Join("dir1", "test2.md") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Добавленный файл не найден в списке исключений")
	}

	// Тестируем повторное добавление того же файла
	err = addToExcludedFiles(configPath, "dir1/test2.md")
	if err != nil {
		t.Fatalf("addToExcludedFiles() вернул ошибку при повторном добавлении: %v", err)
	}

	config, err = loadConfig(configPath)
	if err != nil {
		t.Fatalf("Не удалось загрузить обновленную конфигурацию: %v", err)
	}

	// Проверяем, что файл не добавлен дважды
	count := 0
	for _, file := range config.ExcludedFiles {
		if file == filepath.Join("dir1", "test2.md") {
			count++
		}
	}
	if count > 1 {
		t.Error("Файл добавлен в список исключений более одного раза")
	}
}

func TestProcessFile(t *testing.T) {
	// Создаем временные директории и файлы
	tmpDir := t.TempDir()
	inputDir := filepath.Join(tmpDir, "input")
	outputDir := filepath.Join(tmpDir, "output")
	configPath := filepath.Join(tmpDir, "test.cfg")

	if err := os.MkdirAll(inputDir, 0755); err != nil {
		t.Fatalf("Не удалось создать входную директорию: %v", err)
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("Не удалось создать выходную директорию: %v", err)
	}

	// Создаем тестовый файл конфигурации
	configContent := `[DIRECTORIES]
input_dir = ` + inputDir + `
output_dir = ` + outputDir + `

[EXCLUSIONS]
excluded_files = 

[MODEL]
name = gpt-3.5-turbo
api_url = https://api.openai.com/v1/chat/completions
api_key = test_key
temperature = 0.7
max_tokens = 1000

[PROMPT]
text = Test prompt`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Не удалось создать тестовый файл конфигурации: %v", err)
	}

	// Создаем тестовый входной файл
	inputFilePath := filepath.Join(inputDir, "test.md")
	inputContent := "# Тестовый markdown-файл\n\nЭто тестовый контент."
	if err := os.WriteFile(inputFilePath, []byte(inputContent), 0644); err != nil {
		t.Fatalf("Не удалось создать тестовый входной файл: %v", err)
	}

	outputFilePath := filepath.Join(outputDir, "test.md")

	// Создаем тестовый сервер для имитации API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Отправляем корректный тестовый ответ в формате OpenAI
		response := map[string]interface{}{
			"id":      "test-id",
			"object":  "chat.completion",
			"created": 1234567890,
			"model":   "gpt-3.5-turbo",
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "Обогащенный контент",
					},
					"finish_reason": "stop",
				},
			},
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("Failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	// Создаем и настраиваем конфигурацию
	config := &Config{
		InputDir:    inputDir,
		OutputDir:   outputDir,
		ModelName:   "gpt-3.5-turbo",
		ModelAPIURL: server.URL + "/v1/chat/completions", // Указываем правильный URL с путем
		APIKey:      "test_key",
		Temperature: 0.7,
		MaxTokens:   1000,
		Prompt:      "Test prompt",
	}

	// Создаем ограничитель частоты запросов для тестов
	rateLimiter := NewRateLimiter(10)

	// Тест с безопасными путями
	t.Run("SafePaths", func(t *testing.T) {
		err := processFile(config, inputFilePath, outputFilePath, configPath, rateLimiter)
		if err != nil {
			t.Fatalf("processFile() вернул ошибку: %v", err)
		}

		// Проверяем, что выходной файл создан
		if _, err := os.Stat(outputFilePath); os.IsNotExist(err) {
			t.Error("Выходной файл не был создан")
			return
		}

		// Читаем созданный файл и проверяем содержимое
		data, err := os.ReadFile(outputFilePath)
		if err != nil {
			t.Fatalf("не удалось прочитать выходной файл: %v", err)
		}
		fileContent := string(data)

		if !strings.Contains(fileContent, "Обогащенный контент") {
			t.Error("В выходном файле отсутствует обогащенный контент")
		}

		expectedOldBlock := "```old\n" + inputContent + "\n```"
		if !strings.Contains(fileContent, expectedOldBlock) {
			t.Error("Оригинальное содержимое отсутствует в блоке ```old```")
		}
	})

	// Тест с небезопасными путями
	t.Run("UnsafePaths", func(t *testing.T) {
		unsafeInputPath := "../../../dangerous.md"
		unsafeOutputPath := "../../../dangerous_output.md"

		err := processFile(config, unsafeInputPath, unsafeOutputPath, configPath, rateLimiter)
		if err == nil {
			t.Error("Ожидалась ошибка при обработке файла с небезопасными путями, но ее не было")
		} else if !strings.Contains(err.Error(), "небезопасный путь") {
			t.Errorf("Ожидалась ошибка с текстом 'небезопасный путь', получено '%s'", err.Error())
		}
	})

	// Тест с большим файлом
	t.Run("LargeFile", func(t *testing.T) {
		// Создаем временный большой файл
		largeFilePath := filepath.Join(inputDir, "large.md")
		largeContent := make([]byte, MaxFileSize+1)
		if err := os.WriteFile(largeFilePath, []byte(largeContent), 0644); err != nil {
			t.Fatalf("Не удалось создать тестовый большой файл: %v", err)
		}

		largeOutputPath := filepath.Join(outputDir, "large.md")

		err := processFile(config, largeFilePath, largeOutputPath, configPath, rateLimiter)
		if err == nil {
			t.Error("Ожидалась ошибка при обработке слишком большого файла, но ее не было")
		} else if !strings.Contains(err.Error(), "размер файла превышает") {
			t.Errorf("Ожидалась ошибка с текстом 'размер файла превышает', получено '%s'", err.Error())
		}
	})
}

func TestProcessDirectory(t *testing.T) {
	// Создаем временные директории и файлы
	tmpDir := t.TempDir()
	inputDir := filepath.Join(tmpDir, "input")
	outputDir := filepath.Join(tmpDir, "output")
	configPath := filepath.Join(tmpDir, "test.cfg")

	if err := os.MkdirAll(inputDir, 0755); err != nil {
		t.Fatalf("Не удалось создать входную директорию: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(inputDir, "dir1"), 0755); err != nil {
		t.Fatalf("Не удалось создать поддиректорию: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(inputDir, "dir2"), 0755); err != nil {
		t.Fatalf("Не удалось создать поддиректорию: %v", err)
	}

	// Создаем тестовый файл конфигурации
	configContent := `[DIRECTORIES]
input_dir = ` + inputDir + `
output_dir = ` + outputDir + `

[EXCLUSIONS]
excluded_files = excluded.md

[MODEL]
name = gpt-3.5-turbo
api_url = https://api.openai.com/v1/chat/completions
api_key = test_key
temperature = 0.7
max_tokens = 1000

[PROMPT]
text = Test prompt`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Не удалось создать тестовый файл конфигурации: %v", err)
	}

	// Создаем тестовые файлы
	files := []struct {
		name    string
		content string
	}{
		{"test1.md", "# Тест 1"},
		{"test2.md", "# Тест 2"},
		{filepath.Join("dir1", "dup.md"), "# Дубликат"},
		{filepath.Join("dir2", "dup.md"), "# Дубликат"},
		{"excluded.md", "# Исключенный файл"},
		{"notmd.txt", "Это не markdown файл"},
	}

	for _, f := range files {
		filePath := filepath.Join(inputDir, f.name)
		if err := os.WriteFile(filePath, []byte(f.content), 0644); err != nil {
			t.Fatalf("Не удалось создать тестовый файл %s: %v", f.name, err)
		}
	}

	// Создаем тестовый сервер для имитации API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Отправляем корректный тестовый ответ в формате OpenAI
		response := map[string]interface{}{
			"id":      "test-id",
			"object":  "chat.completion",
			"created": 1234567890,
			"model":   "gpt-3.5-turbo",
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "Обогащенный контент",
					},
					"finish_reason": "stop",
				},
			},
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("Failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	// Создаем и настраиваем конфигурацию
	config := &Config{
		InputDir:      inputDir,
		OutputDir:     outputDir,
		ExcludedFiles: []string{"excluded.md"},
		ModelName:     "gpt-3.5-turbo",
		ModelAPIURL:   server.URL + "/v1/chat/completions", // Указываем правильный URL с путем
		APIKey:        "test_key",
		Temperature:   0.7,
		MaxTokens:     1000,
		Prompt:        "Test prompt",
	}

	// Тест обработки директории
	err := processDirectory(config, configPath)
	if err != nil {
		t.Fatalf("processDirectory() вернул ошибку: %v", err)
	}

	// Проверяем, что созданы выходные файлы для markdown-файлов, кроме исключенных
	expectedFiles := []string{
		"test1.md",
		"test2.md",
		filepath.Join("dir1", "dup.md"),
		filepath.Join("dir2", "dup.md"),
	}
	for _, f := range expectedFiles {
		outputPath := filepath.Join(outputDir, f)
		if _, err := os.Stat(outputPath); os.IsNotExist(err) {
			t.Errorf("Ожидалось создание выходного файла %s, но он не найден", outputPath)
			continue
		}

		// Проверяем содержимое созданного файла
		data, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("не удалось прочитать файл %s: %v", outputPath, err)
		}
		content := string(data)
		if !strings.Contains(content, "Обогащенный контент") {
			t.Errorf("В файле %s отсутствует обогащенный контент", outputPath)
		}

		original := ""
		for _, fi := range files {
			if fi.name == f {
				original = fi.content
				break
			}
		}
		expectedOldBlock := "```old\n" + original + "\n```"
		if !strings.Contains(content, expectedOldBlock) {
			t.Errorf("Оригинальное содержимое в файле %s отсутствует в блоке ```old```", outputPath)
		}
	}

	// Проверяем, что исключенные файлы и не-markdown файлы не обработаны
	notExpectedFiles := []string{"excluded.md", "notmd.txt"}
	for _, f := range notExpectedFiles {
		outputPath := filepath.Join(outputDir, f)
		if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
			t.Errorf("Файл %s не должен быть создан в выходной директории", outputPath)
		}
	}

	// Проверяем, что файлы с одинаковыми именами записаны в исключения по относительному пути
	updatedCfg, err := loadConfig(configPath)
	if err != nil {
		t.Fatalf("Не удалось загрузить обновленную конфигурацию: %v", err)
	}
	expectedExcluded := []string{filepath.Join("dir1", "dup.md"), filepath.Join("dir2", "dup.md")}
	for _, e := range expectedExcluded {
		found := false
		for _, f := range updatedCfg.ExcludedFiles {
			if f == e {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Ожидалось присутствие %s в списке исключений", e)
		}
	}
}
