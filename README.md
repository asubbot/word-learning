# word-learning-cli

CLI-инструмент на Go для изучения иностранных слов через карточки.

## Возможности (MVP)

- Управление колодами: создание и список.
- Управление карточками: добавление, список, удаление, восстановление.
- Режим практики в CLI:
  - `card get` — следующая доступная карточка.
  - `card remember` — скрыть карточку на 24 часа.
  - `card dont-remember` — вернуть карточку в активную ротацию.
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
go run ./cmd/wordcli card add --deck 1 --front "banished" --back "изгнанный" --description "He was banished from the kingdom."
```

### 3) Получить карточку

```bash
go run ./cmd/wordcli card get --deck 1
```

### 4) Отметить как помню (snooze 24h)

```bash
go run ./cmd/wordcli card remember --id 1
```

### 5) Вернуть в активные

```bash
go run ./cmd/wordcli card dont-remember --id 1
```

## Основные команды

### Deck

- `deck create --name --from --to`
- `deck list`

### Card

- `card add --deck --front --back [--description]`
- `card list --deck [--status active|snoozed|removed]`
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

- `card add --deck <deck_id> --front "<text>" --back "<text>" [--description "<text>"]`
  - добавляет карточку в указанную колоду.
- `card list --deck <deck_id> [--status active|snoozed|removed]`
  - выводит карточки колоды;
  - с `--status` фильтрует карточки по статусу.
- `card get --deck <deck_id>`
  - выводит следующую доступную карточку для изучения;
  - показывает только `active` и `snoozed` с истекшим `snoozed_until`;
  - `removed` не участвует в выборке.
- `card remember --id <card_id>`
  - ставит статус `snoozed` на 24 часа.
- `card dont-remember --id <card_id>`
  - устанавливает статус `active` (карточка сразу снова в ротации).
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
