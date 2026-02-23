# План реализации due-date scheduler

## Цель
Перевести выбор следующей карточки с текущего FIFO (`ORDER BY id`) на due-date подход: показывать карточки, для которых наступило время повторения (`next_due_at <= now`).

## Короткий ответ по `snooze`
- **Сейчас убирать `snooze` не стоит** — лучше сделать безопасный переход в 2 этапа.
- **Рекомендация:**
  1. Этап 1: сохранить `status=snoozed` для обратной совместимости, но ввести due-date поля и логику.
  2. Этап 2: постепенно отказаться от `snoozed` и хранить отложенность только через `next_due_at` при `status=active`.

Это снижает риск регрессий и не ломает текущие команды.

## Этап 1. Внедрение due-date без удаления snooze

### 1) Расширить схему БД
Добавить в `cards`:
- `next_due_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP`
- `interval_sec INTEGER NOT NULL DEFAULT 0`
- `ease REAL NOT NULL DEFAULT 2.5`
- `lapses INTEGER NOT NULL DEFAULT 0`
- `last_reviewed_at DATETIME NULL`

Индекс:
- `CREATE INDEX IF NOT EXISTS idx_cards_deck_due ON cards(deck_id, status, next_due_at);`

Проверка:
- Новая БД создается с полями и индексом.
- Существующая БД мигрирует без ошибок.

### 2) Миграция существующих данных
- В `InitSchema` добавить `ALTER TABLE ... ADD COLUMN ...` для новых полей.
- Проставить для старых карточек:
  - `next_due_at = COALESCE(created_at, CURRENT_TIMESTAMP)`
  - `interval_sec=0`, `ease=2.5`, `lapses=0` (если пусто)

Проверка:
- Старые карточки начинают участвовать в выдаче.
- Данные не теряются.

### 3) Изменить выбор следующей карточки
Новый порядок:
- учитывать только карточки текущей колоды,
- исключать `removed`,
- показывать только те, где срок наступил:
  - `status='active' AND next_due_at <= now`
  - (временная совместимость) `status='snoozed' AND snoozed_until <= now`
- сортировка: `ORDER BY next_due_at ASC, id ASC`.

Проверка:
- Просроченные карточки выдаются первыми.
- Непросроченные не выдаются.

### 3.1) Стратегия выбора просроченных карточек
Базовая стратегия в due-date:
- `ORDER BY next_due_at ASC, id ASC` (строгий FIFO по просрочке).

Рекомендуемый компромисс для UX:
- **random-in-top-N**:
  1. SQL выбирает `N` самых просроченных карточек (`ORDER BY next_due_at ASC LIMIT N`),
  2. в приложении случайно выбирается 1 карточка из этого набора.

Почему так:
- сохраняется приоритет просроченных,
- меньше монотонности в выдаче,
- нет starvation, как при полном random.

Рекомендованный старт:
- режим по умолчанию: `strict-fifo`,
- опция конфигурации: `random-top-n` с `N=20`.

Проверка:
- в `strict-fifo` выдача детерминирована,
- в `random-top-n` все карточки выбираются только из top-N,
- старые просроченные карточки не "зависают".

### 4) Правила обновления карточки
#### remember
- `interval_sec` увеличивается:
  - если 0 -> 86400 (1 день)
  - иначе `max(86400, round(interval_sec * ease))`
- `ease = min(2.8, ease + 0.05)`
- `next_due_at = now + interval_sec`
- `last_reviewed_at = now`
- `status=active`

#### dont-remember
- `lapses = lapses + 1`
- `ease = max(1.3, ease - 0.2)`
- `interval_sec = 600` (10 минут)
- `next_due_at = now + 600`
- `last_reviewed_at = now`
- `status=active`

#### remove / restore
- `remove`: как сейчас `status=removed`
- `restore`: `status=active`, `next_due_at=now`

Проверка:
- После `remember` карточка пропадает до срока.
- После `dont-remember` карточка возвращается быстро.

### 5) Строка статистики в `card get`
Считать и выводить:
- `Активных X` = `status='active' AND next_due_at <= now`
- `отложено Y` = `status='active' AND next_due_at > now` + (временно) `status='snoozed'`
- `всего Z` = все карточки колоды без `removed` (или все вообще — выбрать и зафиксировать в README)

Проверка:
- После действий пользователя X/Y меняются ожидаемо.

### 6) Тесты
Добавить/обновить тесты:
- storage: due-date фильтр, интервалы, агрегаты X/Y/Z
- app: remember/dont-remember с проверкой `next_due_at`, `interval_sec`, `ease`, `lapses`
- e2e сценарий в README

Проверка:
- `make check` проходит.
- Coverage не падает.

### 7) Документация
Обновить:
- `README.md`: что такое due-date, как интервал меняется по кнопкам.
- `SPECIFICATION.md`: новые поля и правила планировщика.

Проверка:
- Документация отражает фактическое поведение.

## Этап 2. Опциональное упрощение (убрать snooze)

### Изменения
- Перестать использовать `status='snoozed'` в коде.
- Отложенность хранить только через `next_due_at`.
- Оставить статусы: `active`, `removed`.
- Миграция:
  - для `status='snoozed'` -> `status='active'`
  - `next_due_at = max(next_due_at, snoozed_until)`

### Плюсы
- Меньше состояний и веток логики.
- Единый механизм планирования.

### Риски
- Нужна аккуратная миграция старых записей и тестов.

## Итоговая рекомендация
- **Да, `snooze` лучше убрать, но не сразу.**
- Сначала внедрить due-date совместимо (Этап 1), убедиться в стабильности.
- После этого сделать чистку модели и убрать `snoozed` (Этап 2).
