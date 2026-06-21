# cmd/server

## Metrics Server
Сервер для приёма, хранения и выдачи метрик.

Сервер принимает метрики от агентов по протоколам HTTP и gRPC, аккумулирует их в памяти и предоставляет доступ к текущим значениям. HTTP API реализован с использованием net/http и роутера chi.

Поддерживаемые типы метрик

Сервер работает с двумя типами метрик:

Gauge (float64)

при обновлении новое значение замещает предыдущее

Counter (int64)

при обновлении новое значение прибавляется к уже сохранённому

## HTTP API
### Обновление метрик

````
POST /update/<ТИП_МЕТРИКИ>/<ИМЯ_МЕТРИКИ>/<ЗНАЧЕНИЕ>
Content-Type: text/plain
````

### Пример:

````
POST /update/counter/PollCount/10
````

### Получение значения метрики
````
GET /value/<ТИП_МЕТРИКИ>/<ИМЯ_МЕТРИКИ>
````
### Пример:

````
GET /value/gauge/Alloc
````

### Проверка подключения к БД
````
GET /ping
````

Возвращает `200 OK`, если соединение с PostgreSQL доступно, иначе `500 Internal Server Error`.

## Параметры запуска
Сервер принимает аргументы командной строки через флаги.

| Флаг |	Описание | 	Значение по умолчанию |
| ---- | ------ |:----------------------:|
| -a |	Адрес HTTP-сервера	| localhost:8080  |
| -grpc-a, -grpc-address |	Адрес gRPC-сервера. Если пустой, gRPC-сервер не запускается	| "" |
| -i |	Интервал сохранения в файл (секунды)	| 300 |
| -f |	Путь до файла с метриками	| `%TEMP%/metrics-db.json` |
| -r |	Восстанавливать метрики из файла при старте	| true |
| -d |	DSN подключения к PostgreSQL	| "" |
| -k |	Ключ для HMAC-проверки тела запроса	| "" |
| -crypto-key |	Путь к PEM-файлу с приватным ключом для расшифровки запросов агента	| "" |
| -t |	Доверенная подсеть агентов в CIDR-нотации	| "" |
| -audit-file |	Путь к файлу audit-лога	| "" |
| -audit-url |	URL получателя audit-событий	| "" |
| -c, -config |	Путь к JSON-файлу конфигурации	| "" |

Переменные окружения:
- `ADDRESS`
- `GRPC_ADDRESS`
- `STORE_INTERVAL`
- `STORE_FILE` или `FILE_STORAGE_PATH`
- `RESTORE`
- `DATABASE_DSN` (имеет приоритет над `-d`)
- `KEY`
- `CRYPTO_KEY` — путь к PEM-файлу с приватным ключом
- `TRUSTED_SUBNET` — доверенная подсеть агентов в CIDR-нотации
- `AUDIT_FILE`
- `AUDIT_URL`
- `CONFIG` — путь к JSON-файлу конфигурации

Значения применяются в порядке приоритета: переменные окружения, флаги, JSON-файл, значения по умолчанию.

Пример JSON-конфигурации:
```json
{
  "address": "localhost:8080",
  "grpc_address": "",
  "restore": true,
  "store_interval": "1s",
  "store_file": "/path/to/file.db",
  "database_dsn": "",
  "key": "",
  "crypto_key": "/path/to/private.pem",
  "trusted_subnet": "",
  "audit_file": "",
  "audit_url": ""
}
```

Пример генерации пары ключей:
````
openssl genrsa -out private.pem 2048
openssl rsa -in private.pem -pubout -out public.pem
````

## Выбор хранилища
Порядок выбора backend при старте:
1. PostgreSQL, если `DATABASE_DSN` или `-d` заданы и не пустые.
2. Файл, если заданы файловые настройки (`STORE_FILE`/`FILE_STORAGE_PATH`/`STORE_INTERVAL`/`RESTORE`, `-f`/`-i`/`-r` или JSON-поля `store_file`/`store_interval`/`restore`).
3. Память, если не задан ни PostgreSQL, ни файловый режим.
