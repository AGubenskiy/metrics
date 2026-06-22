# cmd/agent
## Metrics Agent

Агент для сбора метрик и их отправки на сервер метрик.

Агент периодически:

опрашивает пакет runtime и собирает метрики

отправляет метрики батчами на HTTP-сервер методом POST или на gRPC-сервер методом `UpdateMetrics`
Отправка метрик

Каждый запрос отправки метрик содержит заголовок `X-Real-IP` с IP-адресом хоста агента.
Для gRPC тот же IP передаётся в metadata с ключом `x-real-ip`.

### Метрики отправляются на сервер по HTTP методом POST в формате:
````
POST /update/<ТИП_МЕТРИКИ>/<ИМЯ_МЕТРИКИ>/<ЗНАЧЕНИЕ>
Content-Type: text/plain
````

### Пример:
````
POST /update/counter/PollCount/5
````
## Адрес сервера по умолчанию:
````
http://localhost:8080
````

Параметры запуска


## Агент принимает аргументы командной строки через флаги.
| Флаг | Описание | Значение по умолчанию|
| ----- |--------|:--------:|
|-a|	Адрес HTTP-сервера	| localhost:8080|
|-grpc-a, -grpc-address|	Адрес gRPC-сервера. Если задан, батчи отправляются по gRPC	| ""|
|-r |	Интервал отправки метрик (сек)|	10|
|-p	 |Интервал опроса runtime-метрик (сек)|	2|
|-l	 |Максимальное число одновременных исходящих запросов|	1|
|-k	 |Ключ для HMAC-подписи тела запроса|	""|
|-crypto-key	 |Путь к PEM-файлу с публичным ключом для шифрования запросов|	""|
|-grpc-cert-file	 |Путь к PEM-файлу сертификата/CA для проверки TLS gRPC-сервера|	""|
|-c, -config	 |Путь к JSON-файлу конфигурации|	""|

Переменные окружения имеют приоритет над флагами:
- `ADDRESS`
- `GRPC_ADDRESS`
- `REPORT_INTERVAL`
- `POLL_INTERVAL`
- `RATE_LIMIT`
- `KEY`
- `CRYPTO_KEY` — путь к PEM-файлу с публичным ключом
- `GRPC_CERT_FILE` — путь к PEM-файлу сертификата/CA для проверки TLS gRPC-сервера
- `CONFIG` — путь к JSON-файлу конфигурации

Значения применяются в порядке приоритета: переменные окружения, флаги, JSON-файл, значения по умолчанию.

Пример JSON-конфигурации:
```json
{
  "address": "localhost:8080",
  "grpc_address": "",
  "report_interval": "1s",
  "poll_interval": "1s",
  "rate_limit": 1,
  "key": "",
  "crypto_key": "/path/to/public.pem",
  "grpc_cert_file": "/path/to/grpc-cert.pem"
}
```

### Пример запуска:
````
-a=localhost:8080 -r=5 -p=1
````

### Пример генерации ключей:
````
openssl genrsa -out private.pem 2048
openssl rsa -in private.pem -pubout -out public.pem
````

### Пример генерации самоподписанного сертификата для gRPC TLS:
````
openssl req -x509 -newkey rsa:2048 -nodes -days 365 -keyout grpc-key.pem -out grpc-cert.pem -subj "/CN=localhost" -addext "subjectAltName=DNS:localhost,IP:127.0.0.1"
````

При использовании gRPC агенту нужно передать `grpc-cert.pem` через `-grpc-cert-file`, `GRPC_CERT_FILE` или поле JSON-конфигурации `grpc_cert_file`.
