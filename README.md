# word-learning-cli

CLI-инструмент на Go для изучения иностранных слов через карточки.

## Возможности (MVP)

- Управление колодами: создание и список.
- Управление карточками: добавление, список, удаление, восстановление.
- Режим практики в CLI:
  - `card get` — следующая доступная карточка.
  - `card remember` — увеличить интервал до следующего показа (due-date scheduler).
  - `card dont-remember` — сократить интервал до короткого повтора (10 минут).
- Локальное надежное хранение в SQLite.

## Требования

- Go 1.22+ (проверено в среде проекта).

## Установка зависимостей

```bash
go mod tidy
```

## Быстрый старт

### 1) Создать колоду

```bash
go run ./cmd/wordcli deck create --name "English Basics" --from EN --to RU
```

### 2) Добавить карточку

```bash
go run ./cmd/wordcli card add --deck 1 --front "banished" --back "изгнанный" --pronunciation "/banished/" --description "He was banished from the kingdom."
```

### 3) Получить карточку

```bash
go run ./cmd/wordcli card get --deck 1
```

### 4) Отметить как помню (увеличить интервал)

```bash
go run ./cmd/wordcli card remember --id 1
```

### 5) Отметить как не помню (быстрый повтор)

```bash
go run ./cmd/wordcli card dont-remember --id 1
```

## Основные команды

### Deck

- `deck create --name --from --to`
- `deck list`

### Card

- `card add --deck --front --back [--pronunciation] [--description]`
- `card list --deck [--status active|snoozed|removed]` (статус `snoozed` сохранен для совместимости со старыми данными)
- `card get --deck`
- `card remember --id`
- `card dont-remember --id`
- `card remove --id`
- `card restore --id`

## Описание CLI-команд

### Глобальные флаги

- `--db <path>` — путь к SQLite-файлу БД (по умолчанию `wordcli.db` в текущей директории).
- `-h, --help` — показать справку.

### Deck

- `deck create --name <name> --from <lang> --to <lang>`
  - создает новую колоду;
  - `--from` и `--to` принимают языковой код из 2-8 латинских букв (например `EN`, `RU`);
  - языки источника и назначения должны отличаться.
- `deck list`
  - выводит список существующих колод.

### Card

- `card add --deck <deck_id> --front "<text>" --back "<text>" [--pronunciation "<text>"] [--description "<text>"]`
  - добавляет карточку в указанную колоду.
  - `--pronunciation` опционально сохраняет транскрипцию/подсказку произношения.
- `card list --deck <deck_id> [--status active|snoozed|removed]`
  - выводит карточки колоды;
  - с `--status` фильтрует карточки по статусу.
- `card get --deck <deck_id>`
  - выводит следующую доступную карточку для изучения;
  - показывает карточки по due-date (`next_due_at <= now`);
  - legacy-поддержка: `snoozed` с истекшим `snoozed_until` тоже участвуют в выдаче;
  - `removed` не участвует в выборке.
  - после карточки печатает сводку: `Активных X, отложено Y, всего Z`.
- `card remember --id <card_id>`
  - увеличивает интервал повторения и переносит карточку в будущее (`next_due_at`).
- `card dont-remember --id <card_id>`
  - уменьшает интервал и ставит короткий повтор через 10 минут.
- `card remove --id <card_id>`
  - мягко удаляет карточку из активной ротации (`status=removed`).
- `card restore --id <card_id>`
  - восстанавливает карточку в статус `active`.

## Работа с БД

По умолчанию используется файл `wordcli.db` в текущей директории. Можно задать путь явно:

```bash
go run ./cmd/wordcli --db ./data/mywords.db deck list
```

## Проверки качества

```bash
go test ./...
go vet ./...
```

## Запуск CLI

```bash
go run ./cmd/wordcli
```

Примеры запуска подкоманд:

```bash
go run ./cmd/wordcli completion --help
go run ./cmd/wordcli deck list
go run ./cmd/wordcli card get --deck 1
```

## Автокомплит (Cobra completion)

`wordcli` поддерживает генерацию скриптов автодополнения через встроенную команду:

```bash
go run ./cmd/wordcli completion --help
```

Подключение в текущую сессию:

```bash
# bash
source <(go run ./cmd/wordcli completion bash)

# zsh
source <(go run ./cmd/wordcli completion zsh)

# fish
go run ./cmd/wordcli completion fish | source
```

После этого по `Tab` должны подсказываться команды и флаги (`card`, `deck`, `--db` и т.д.).

Постоянная настройка для `zsh`:

```bash
echo 'source <(go run ./cmd/wordcli completion zsh)' >> ~/.zshrc
source ~/.zshrc
```

Команду выше выполняйте из корня проекта.

## E2E сценарий ручной проверки

Ниже минимальный сценарий для проверки полного потока в чистой БД:

```bash
# 1) Очистить тестовую БД (если была)
rm -f ./e2e.db

# 2) Создать колоду
go run ./cmd/wordcli --db ./e2e.db deck create --name "English Basics" --from EN --to RU

# 3) Добавить карточку
go run ./cmd/wordcli --db ./e2e.db card add --deck 1 --front "banished" --back "изгнанный" --pronunciation "/banished/" --description "He was banished from the kingdom."

# 4) Убедиться, что карточка в active
go run ./cmd/wordcli --db ./e2e.db card list --deck 1 --status active

# 5) Получить следующую карточку
go run ./cmd/wordcli --db ./e2e.db card get --deck 1

# 6) Пометить как remember (карточка уходит в будущий due)
go run ./cmd/wordcli --db ./e2e.db card remember --id 1

# 7) Проверить, что карточка временно не выдается (due еще не наступил)
go run ./cmd/wordcli --db ./e2e.db card get --deck 1

# 8) Отметить как не помню (ставим короткий повтор)
go run ./cmd/wordcli --db ./e2e.db card dont-remember --id 1

# 9) Сразу после dont-remember карточка еще может не выдаваться (ожидаем короткую паузу ~10 минут)
go run ./cmd/wordcli --db ./e2e.db card get --deck 1

# 10) Удалить карточку из ротации
go run ./cmd/wordcli --db ./e2e.db card remove --id 1

# 11) Проверить removed-список
go run ./cmd/wordcli --db ./e2e.db card list --deck 1 --status removed

# 12) Восстановить карточку
go run ./cmd/wordcli --db ./e2e.db card restore --id 1

# 13) Финальная проверка active-списка
go run ./cmd/wordcli --db ./e2e.db card list --deck 1 --status active
```

## Команды через Makefile

```bash
make help
make fmt
make test
make vet
make lint
make coverage
make coverage-html
make check
```

`make lint` использует `golangci-lint`; если он не установлен, команда подскажет ссылку на установку.
`make coverage` печатает текстовую сводку покрытия, `make coverage-html` генерирует файл `coverage.html`.
`make check` запускает `fmt + vet + lint + coverage`.

## Структура проекта

- `cmd/wordcli` — точка входа CLI.
- `internal/cli` — команды Cobra и форматирование вывода.
- `internal/app` — бизнес-правила и валидация.
- `internal/storage/sqlite` — SQLite-хранилище и SQL-операции.
- `internal/domain` — доменные модели.
- `migrations` — SQL-схема для инициализации.
