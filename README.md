# Task Tracker API

[![CI](https://github.com/lumen-dev02/task-tracker-api/actions/workflows/ci.yml/badge.svg)](https://github.com/lumen-dev02/task-tracker-api/actions/workflows/ci.yml)

Учебный REST API для личных задач на Go. Пользователь регистрируется, получает JWT и управляет только своими задачами. Проект сделан для практики HTTP, SQL, аутентификации, проверки прав и интеграционного тестирования. Это не описание коммерческого продукта или опыта работы.

В отличие от [Team Task Manager](https://github.com/lumen-dev02/team-task-manager), здесь нет команд, проектов, участников и ролей. Разграничение доступа основано на владении задачей: без токена доступ закрыт, с токеном доступны только собственные записи.

## Возможности

- Регистрация с уникальным email и хешированием пароля через bcrypt.
- Вход и JWT access-токен сроком на один час.
- Создание, получение, полное обновление и удаление своих задач.
- Статусы `todo`, `in_progress`, `done`.
- Фильтр по статусу, пагинация `limit`/`offset`, сортировка от новых задач к старым.
- PostgreSQL с ограничениями целостности и индексами для списков задач.
- Транзакционная начальная миграция с защитой от одновременного запуска.
- Проверки liveness/readiness и завершение HTTP по `SIGINT`/`SIGTERM`.
- Unit-тесты и интеграционные HTTP-тесты с настоящим PostgreSQL; GitHub Actions.

## Стек

Go 1.26+, стандартный `net/http`, PostgreSQL 17, `pgx/v5`, `golang-jwt/jwt/v5`, bcrypt из `golang.org/x/crypto`. Docker Compose поднимает API, БД и одноразовый запуск миграции. ORM, Redis и брокер сообщений не нужны для текущих требований.

## Быстрый запуск

Понадобятся Git, Docker с Compose v2 и OpenSSL. Для запуска через Docker локальный Go не требуется.

```bash
git clone https://github.com/lumen-dev02/task-tracker-api.git
cd task-tracker-api
cp .env.example .env
openssl rand -hex 32
```

Вставьте результат последней команды в `JWT_SECRET` файла `.env`. Приложение намеренно отклоняет шаблонный секрет. Пароль PostgreSQL из примера предназначен только для локальной разработки. Если меняете его, используйте URL-safe значение, например hex: пароль подставляется в URL подключения.

```bash
docker compose up --build -d
docker compose ps -a
curl --retry 20 --retry-connrefused --retry-delay 1 --fail http://127.0.0.1:8081/readyz
```

Compose ждёт готовности PostgreSQL, выполняет миграцию и запускает API. Для сервиса `migrate` состояние `Exited (0)` нормально. API доступен на `127.0.0.1:8081`, БД — на `127.0.0.1:55433`; другие порты можно задать в `.env`. Привязка к localhost — намеренная: API не подготовлен для открытой публичной регистрации.

```bash
docker compose logs api migrate
docker compose down
```

Обычный `down` сохраняет данные в volume. Команда `docker compose down -v` **удаляет данные проекта**; используйте её только для намеренного сброса локальной БД. Изменение реквизитов в `.env` не изменяет пользователей уже созданной базы в существующем volume.

### Запуск приложения без Docker

Понадобятся Go 1.26+ и PostgreSQL. Базу можно оставить в Compose:

```bash
docker compose up -d --wait db
export DATABASE_URL='postgres://tracker:local-development-only@127.0.0.1:55433/tasktracker?sslmode=disable'
export JWT_SECRET="$(openssl rand -hex 32)"
go run ./cmd/api -migrate
go run ./cmd/api
```

Предварительно остановите контейнер API (`docker compose stop api`), если порт `8081` уже занят. URL выше соответствует исходным значениям `.env.example`; при изменении реквизитов обновите URL. Go-приложение **не загружает `.env` автоматически** — оно читает окружение. Compose, наоборот, использует `.env` для подстановки значений.

`-migrate` выполняет миграцию и выходит; JWT-секрет этому режиму не нужен. Повторный запуск безопасен. Обычный запуск сам схему не меняет и завершается ошибкой, если начальная миграция не применена.

## Пример API

```bash
curl --fail-with-body -sS http://127.0.0.1:8081/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"timur@example.com","name":"Timur","password":"local-demo-password"}'

curl --fail-with-body -sS http://127.0.0.1:8081/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"timur@example.com","password":"local-demo-password"}'
```

Скопируйте `access_token` из ответа входа:

```bash
export TOKEN='<access_token>'
curl --fail-with-body -i http://127.0.0.1:8081/api/v1/tasks \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"title":"Write API tests","description":"Check task permissions"}'

# Подставьте id созданной задачи вместо 1.
curl --fail-with-body -sS -X PUT http://127.0.0.1:8081/api/v1/tasks/1 \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"title":"Write API tests","description":"Check task permissions","status":"done"}'

curl --fail-with-body -sS 'http://127.0.0.1:8081/api/v1/tasks?status=done&limit=20&offset=0' \
  -H "Authorization: Bearer $TOKEN"
```

Пароль в примере — только демонстрационный. Полное описание маршрутов, полей, ответов и ограничений: [справочник API](docs/api.md).

### Повторяемый сценарий с двумя пользователями

Нужны запущенный API, `curl`, `jq` и `openssl`:

```bash
bash scripts/demo.sh
# Другой адрес:
BASE_URL=http://127.0.0.1:8082 bash scripts/demo.sh
```

Сценарий регистрирует двух пользователей с уникальными адресами, создаёт задачу первого, проверяет `404` при чтении вторым и переводит задачу в `done`. Тестовые аккаунты и задача остаются в БД; пароли и токены не выводятся.

## Конфигурация

| Переменная приложения | Значение по умолчанию | Назначение |
|---|---|---|
| `DATABASE_URL` | Обязательна | Подключение PostgreSQL |
| `JWT_SECRET` | Обязательна для API | Не менее 32 байт, не шаблон; используйте случайное значение |
| `HTTP_ADDR` | `127.0.0.1:8081` | Listen address; в Compose задаётся `:8080` |

В `.env` для Compose также доступны `APP_PORT`, `DB_PORT`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`. Это настройки Compose, а не отдельные настройки Go-приложения. Не публикуйте `.env` и JWT-секрет.

Настройки в коде: пул до 10 соединений; контекст запроса 5 секунд; чтение заголовков 5 секунд; чтение/запись HTTP 10 секунд; graceful shutdown 10 секунд. Контекст отменяет работу с БД, но не может прервать bcrypt посередине вычисления.

## Проверки

```bash
go test -race -count=1 -timeout=120s ./...
go vet ./...
go build -o bin/api ./cmd/api
```

Без `TEST_DATABASE_URL` PostgreSQL-тесты **пропускаются**, а не считаются проверенными. Для полного прогона запустите БД и задайте URL тестовой базы. Например, для локального учебного Compose:

```bash
TEST_DATABASE_URL='postgres://tracker:local-development-only@127.0.0.1:55433/tasktracker?sslmode=disable' \
  go test -race -count=1 -timeout=120s ./...
```

Тесты создают уникальные схемы и удаляют только их, не трогая таблицы приложения. Нужны права `CREATE` в указанной БД. Несмотря на изоляцию схем, не направляйте тесты в production; предпочтительна отдельная тестовая БД. Можно использовать `make test`, `make integration`, `make vet`, `make build`; цель `integration` требует явно заданный URL.

Проверяются JWT и их ограничения, хеширование и валидация, HTTP-ошибки, регистрация/вход, CRUD, изоляция пользователей при чтении/изменении/удалении/списках, фильтры, пагинация, конкурентная регистрация одного email, повторный/конкурентный запуск и откат миграции. В CI PostgreSQL-тесты обязательны; дополнительно собирается Docker-образ. Race detector проверяет исполненные сценарии, а не доказывает отсутствие всех возможных гонок.

## Структура и решения

```text
cmd/api/           конфигурация запуска, подключение БД, HTTP и сигналы
internal/auth/     выпуск и проверка JWT
internal/config/   чтение переменных окружения
internal/httpapi/  маршруты, JSON, аутентификация и HTTP-ошибки
internal/model/    структуры данных
internal/service/  валидация, пароли, сценарии работы с задачами
internal/store/    SQL-запросы PostgreSQL
internal/testutil/ изолированные схемы для интеграционных тестов
migrations/        SQL и runner начальной миграции
scripts/           демонстрационный сценарий
```

- HTTP не содержит SQL и не выбирает владельца из тела запроса: ID пользователя берётся только из проверенного JWT.
- Каждый SQL-запрос к задачам ограничен владельцем. Несуществующая и чужая задачи дают одинаковый `404`.
- Интерфейс репозитория объявлен у использующего его service-слоя; в рабочем приложении реализация одна. Нет универсального ORM/repository framework.
- Статусы и ограничения полей проверяются в сервисе и дополнительно в БД.

Подробнее о схеме, безопасности и компромиссах: [архитектура](docs/architecture.md).

## Ограничения

- Нет совместного доступа, администраторов, проектов, вложений и уведомлений.
- Нет refresh-токенов, logout/revocation, восстановления пароля и подтверждения email. Адрес используется как идентификатор, не как доказанный контакт.
- Нет rate limiting и HTTPS. Для публичного размещения нужны TLS, ограничение запросов, защита регистрации/входа от перебора, управление секретами и резервные копии. Локальные пароли и `sslmode=disable` не предназначены для внешней сети.
- Уникальность email регистронезависимая за счёт нормализации в lowercase. `409` при регистрации сообщает о занятости адреса; это сознательно простой контракт учебной версии.
- `PUT` заменяет редактируемые поля целиком; конкурирующие обновления используют last-write-wins. Нет истории и проверки версии.
- Offset-пагинация может смещаться при одновременных добавлениях и удалениях; общего `total` в ответе нет. Глобальная квота задач на пользователя не задана.
- Runner поддерживает одну начальную миграцию, без автоматического downgrade. Для дальнейшего развития схемы нужен версионированный процесс миграций, а не редактирование уже применённого SQL.
