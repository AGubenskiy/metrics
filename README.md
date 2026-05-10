# go-musthave-metrics-tpl

Шаблон репозитория для трека «Сервер сбора метрик и алертинга».

## Начало работы

1. Склонируйте репозиторий в любую подходящую директорию на вашем компьютере.
2. В корне репозитория выполните команду `go mod init <name>` (где `<name>` — адрес вашего репозитория на GitHub без префикса `https://`) для создания модуля.

## Обновление шаблона

Чтобы иметь возможность получать обновления автотестов и других частей шаблона, выполните команду:

```
git remote add -m v2 template https://github.com/Yandex-Practicum/go-musthave-metrics-tpl.git
```

Для обновления кода автотестов выполните команду:

```
git fetch template && git checkout template/v2 .github
```

Затем добавьте полученные изменения в свой репозиторий.

## Запуск автотестов

Для успешного запуска автотестов называйте ветки `iter<number>`, где `<number>` — порядковый номер инкремента. Например, в ветке с названием `iter4` запустятся автотесты для инкрементов с первого по четвёртый.

При мёрже ветки с инкрементом в основную ветку `main` будут запускаться все автотесты.

Подробнее про локальный и автоматический запуск читайте в [README автотестов](https://github.com/Yandex-Practicum/go-autotests).

## Структура проекта

Приведённая в этом репозитории структура проекта является рекомендуемой, но не обязательной.

Это лишь пример организации кода, который поможет вам в реализации сервиса.

При необходимости можно вносить изменения в структуру проекта, использовать любые библиотеки и предпочитаемые структурные паттерны организации кода приложения, например:
- **DDD** (Domain-Driven Design)
- **Clean Architecture**
- **Hexagonal Architecture**
- **Layered Architecture**

## Бенчмарки

Добавлены бенчмарки:

- `internal/storage`: `BenchmarkMemStorageUpdateMetrics`, `BenchmarkMemStorageSnapshotLarge`, `BenchmarkMemStorageSaveToFileLarge`
- `internal/handler`: `BenchmarkHandlerUpdateMetricsJSON`
- `internal/agent`: `BenchmarkAgentCollectMetricsBatch`

Команды запуска:

```bash
go test -run ^$ -bench BenchmarkMemStorage -benchmem ./internal/storage
go test -run ^$ -bench BenchmarkHandlerUpdateMetricsJSON -benchmem ./internal/handler
go test -run ^$ -bench BenchmarkAgentCollectMetricsBatch -benchmem ./internal/agent
```

Текущие результаты:

```text
BenchmarkMemStorageUpdateMetrics-28      	  235285	      5107 ns/op	       0 B/op	       0 allocs/op
BenchmarkMemStorageSnapshotLarge-28      	    1681	    710716 ns/op	  331913 B/op	       6 allocs/op
BenchmarkMemStorageSaveToFileLarge-28    	     457	   2865947 ns/op	  711220 B/op	      28 allocs/op
BenchmarkHandlerUpdateMetricsJSON-28    	   24585	     51027 ns/op	   67864 B/op	     607 allocs/op
BenchmarkAgentCollectMetricsBatch-28    	   16636	     76294 ns/op	  331777 B/op	       3 allocs/op
```

Для профилирования памяти добавлен helper:

- `cmd/profilemem` — создаёт реалистичный набор данных (`4096` gauge, `512` counter), многократно вызывает `SaveToFile` и сохраняет heap profile.

Профили сохранены в директории `profiles`:

- `profiles/base.pprof` — профиль до оптимизации
- `profiles/result.pprof` — профиль после оптимизации

Базовый профиль снимался командой:

```bash
go run ./cmd/profilemem -output ./profiles/base.pprof -iterations 1000
```

При анализе `pprof` использовались команды `top`, `list`, `peek`, `web`.

Выводы по `base.pprof`:

- `top -sample_index=alloc_space`: основной потребитель памяти — `internal/storage.(*MemStorage).snapshot` (`387.94MB`, `49.18%`), далее `go-json` marshaling (`218.75MB`, `27.73%`).
- `top -sample_index=alloc_objects`: почти все объекты создавались в `snapshot` (`2,307,199`, `98.78%`).
- `list snapshot`: самые тяжёлые строки — `make([]models.Metrics, ...)`, промежуточные `gaugeNames`/`counterNames` и escaping локальных `value`/`delta`.
- `peek snapshot`: горячий путь идёт из `SaveToFile` в `snapshot`.
- `web`: не отработал в локальном окружении, потому что не установлен Graphviz (`dot` отсутствует в `PATH`).

Что было оптимизировано:

- в `internal/storage.(*MemStorage).snapshot` убраны промежуточные слайсы имён;
- локальные `value`/`delta`, из-за которых возникала отдельная heap-аллокация на каждую метрику, заменены на плотные backing-слайсы `gaugeValues` и `counterValues`;
- сортировка перенесена на итоговый `[]models.Metrics`;
- тот же приём с backing-слайсами применён в `internal/agent.(*Agent).collectMetricsBatch`.

Эффект по бенчмаркам:

```text
BenchmarkMemStorageSnapshotLarge: 406792 B/op, 4611 allocs/op -> 331913 B/op, 6 allocs/op
BenchmarkMemStorageSaveToFileLarge: 915510 B/op, 4636 allocs/op -> 711220 B/op, 28 allocs/op
BenchmarkAgentCollectMetricsBatch: 331778 B/op, 4609 allocs/op -> 331777 B/op, 3 allocs/op
```

После оптимизации профиль снимался командой:

```bash
go run ./cmd/profilemem -output ./profiles/result.pprof -iterations 1000
```

`top -sample_index=alloc_space` для `result.pprof` показал снижение общего объёма аллокаций с `788.83MB` до `582.32MB`, а вклад `snapshot` снизился с `387.94MB` до `316.43MB`.

Точный вывод команды:

```bash
go tool pprof -top -diff_base=profiles/base.pprof profiles/result.pprof
```

```text
File: profilemem.exe
Type: inuse_space
Showing nodes accounting for 1.47kB, 0.18% of 813.84kB total
      flat  flat%   sum%        cum   cum%
    1.69kB  0.21%  0.21%     1.69kB  0.21%  runtime.acquireSudog
   -0.28kB 0.035%  0.17%    -0.28kB 0.035%  github.com/goccy/go-json/internal/encoder.appendNormalizedHTMLString
   -0.09kB 0.012%  0.16%    -0.09kB 0.012%  github.com/AGubenskiy/metrics/internal/storage.(*MemStorage).snapshot
    0.09kB 0.012%  0.17%     0.09kB 0.012%  strconv.fmtF
   -0.08kB 0.0096%  0.16%    -0.16kB  0.02%  os.CreateTemp
    0.06kB 0.0077%  0.17%     0.16kB  0.02%  os.MkdirTemp
```
