# Development tips

See README.md for detailed instructions. The repository uses `Taskfile.yml` to manage common operations. Frequently used commands:

- `task`              – список всех задач
- `task setup`        – создание рабочих директорий
- `task fmt`          – форматирование исходников
- `task vet`          – статический анализ
- `task test`         – запуск тестов
- `task test-coverage` – тесты с покрытием
- `task build` / `task run` / `task run-build` – сборка и запуск
- `task check`        – fmt + vet + test
- `task ci`           – fmt-check + vet + test-coverage

The application reads settings from `rich.cfg` (use `-config` to override). The `[PROMPT]` section begins as:

```
text = """Focus on the task at hand, ignoring all previously establish
```

Source markdown files live in `input_dir` (by default `./todo`), and results are written to `output_dir` (`./done`). API keys can be specified directly in the config or via environment variables.

For day‑to‑day work, run:

```
task setup   # create dirs
task run     # run the CLI with ./rich -config rich.cfg
```

Unit tests live in `main_test.go`. The README lists all available tasks, for example:

```
# Показать все доступные задачи
task

# Настройка проекта (создание директорий)
task setup
...
# Сборка и запуск
task run-build
```

This file helps new contributors quickly locate development commands and understand how to run the tool.

