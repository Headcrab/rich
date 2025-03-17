package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
temperature = 0.8
max_tokens = 2000

[PROMPT]
text = Test prompt`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Не удалось создать тестовый файл конфигурации: %v", err)
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

	content := "Тестовый контент"
	enriched, err := enrichContent(config, content)
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

			_, err := enrichContent(config, "Test content")
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

func TestAddToExcludedFiles(t *testing.T) {
	// Создаем временный файл конфигурации
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test.cfg")

	configContent := `[EXCLUSIONS]
excluded_files = test1.md`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Не удалось создать тестовый файл конфигурации: %v", err)
	}

	// Тестируем добавление нового файла
	err := addToExcludedFiles(configPath, "test2.md")
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
		if file == "test2.md" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Добавленный файл не найден в списке исключений")
	}

	// Тестируем повторное добавление того же файла
	err = addToExcludedFiles(configPath, "test2.md")
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
		if file == "test2.md" {
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

	if err := os.MkdirAll(inputDir, 0755); err != nil {
		t.Fatalf("Не удалось создать входную директорию: %v", err)
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("Не удалось создать выходную директорию: %v", err)
	}

	// Создаем тестовый файл конфигурации
	configPath := filepath.Join(tmpDir, "test.cfg")
	configContent := `[DIRECTORIES]
input_dir = ` + inputDir + `
output_dir = ` + outputDir + `

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

	// Создаем тестовый markdown файл
	inputFile := filepath.Join(inputDir, "test.md")
	if err := os.WriteFile(inputFile, []byte("# Test Content"), 0644); err != nil {
		t.Fatalf("Не удалось создать тестовый markdown файл: %v", err)
	}

	// Загружаем конфигурацию
	config, err := loadConfig(configPath)
	if err != nil {
		t.Fatalf("Не удалось загрузить конфигурацию: %v", err)
	}

	// Создаем тестовый сервер для имитации API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	config.ModelAPIURL = server.URL + "/v1/chat/completions"

	// Тестируем обработку файла
	outputFile := filepath.Join(outputDir, "test.md")
	err = processFile(config, inputFile, outputFile, configPath)
	if err != nil {
		t.Fatalf("processFile() вернул ошибку: %v", err)
	}

	// Проверяем, что выходной файл создан
	if _, err := os.Stat(outputFile); os.IsNotExist(err) {
		t.Error("Выходной файл не создан")
	}

	// Проверяем содержимое выходного файла
	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Не удалось прочитать выходной файл: %v", err)
	}

	if string(content) == "" {
		t.Error("Выходной файл пуст")
	}
}
