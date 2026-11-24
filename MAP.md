# Карта проекта

HAProxy Bridge Manager — локальное терминальное приложение на Go для редактирования TCP-маршрутов HAProxy, проверки и применения конфигурации в Docker или через systemd. Интерфейс построен на Bubble Tea, Bubbles и Lip Gloss.

| Что ищем | Путь от корня проекта | Ориентир |
| --- | --- | --- |
| Точка входа | `cmd/haproxy-bridge/main.go` | `main` читает `--settings` и `--version`, запускает `tui.New` в альтернативном экране терминала. |
| Экраны, клавиши и оформление | `internal/tui/tui.go`, `internal/tui/presentation.go`, `internal/tui/dashboard.go` | `Model.Update` обрабатывает события, `Model.View` выбирает экран, `routesView` рисует список маршрутов, `dashboardView` оформляет широкую раскладку и метки статусов. |
| Редактирование и копирование маршрута | `internal/tui/route_editor.go`, `internal/tui/forms.go`, `internal/tui/presentation.go` | `openRoute` открывает черновик, `routeEditorView` показывает панели параметров и серверов, `openServer` и `storeServer` обслуживают форму сервера. `candidate` собирает изменения для предпросмотра diff. |
| Мастер первого запуска и настройки | `internal/tui/forms.go`, `internal/settings/settings.go` | `New`, `beginPreferences`, `savePreferences` ведут сценарий TUI. `settings.Path`, `Load`, `Settings.Save` определяют путь, чтение и запись TOML. Пример в `config.example.toml`. |
| Чтение и изменение `haproxy.cfg` | `internal/haproxy/{document,model,mutate,reorder,validate}.go` | `Parse` и `Document.Bytes` читают и собирают исходный текст. `UpdateFrontend`, `UpdateServer`, `ReorderServers` и `Document.Validate` отвечают за изменения и проверку. `CopyRoute` дублирует секции маршрута, `NextFreePort` ищет следующий свободный порт по `frontend` и `listen`. |
| Проверка, сохранение и применение | `internal/app/session.go` | `Open` загружает документ и хеш файла. `Session.Validate` проверяет модель и запускает внешний валидатор. `Session.Save` записывает файл. `Session.Apply` применяет сохранённую версию, проверяет TCP-слушатели и при сбое может откатиться к подтверждённой точке текущего сеанса. |
| Docker и systemd | `internal/platform/manager.go` | `Docker.Inspect`, `Docker.Validate`, `Docker.Apply` работают с контейнером и проверяют видимость конфига. `Native.Validate`, `Native.CheckServiceConfig`, `Native.Apply` обслуживают локальный HAProxy и systemd. |
| Запись, резервные копии и восстановление | `internal/storage/storage.go`, `internal/backup/backup.go` | `Writer.WriteWithOptions` проверяет хеш и атомарно заменяет файл. `Manager.Create`, `Manager.Restore` и `Manager.Prune` отвечают за копии, восстановление и ротацию. После восстановления `Session.Restore` перечитывает документ. |
| Runtime API, диагностика и журнал | `internal/monitor/runtime.go`, `internal/tui/route_monitor.go`, `internal/network/probe.go`, `internal/app/session.go` | `monitor.Read` читает строки backend и серверов из Unix-сокета. `routeDetails` выводит состояние маршрута, переход на резерв, сессии и диагностику. `network.Probe` выполняет TCP/TLS-проверки, `Session.Log` пишет журнал. |
| Запуск, сборка и тесты | `Makefile`, `.github/workflows/test.yml`, `integration/docker_test.go` | `make build` → `bin/haproxy-bridge`. `make test` запускает `go test -race ./...` и `go vet ./...`. `make integration` включает Docker-тесты HAProxy 3.2. `make release` собирает Linux amd64/arm64 и контрольные суммы. CI запускает тесты, интеграцию и сборку релиза. |
| Локальный стенд | `lab/compose.yaml`, `lab/haproxy.cfg`, `lab/run.py` | `make lab` запускает три тестовых контейнера, Unix-сокет статистики на хосте и TUI. Рабочая копия конфигурации сохраняется в `lab/.state/`. |

**Документация.** [README.md](README.md) описывает запуск, клавиши и эксплуатацию. [docs/SAFETY.md](docs/SAFETY.md) фиксирует инварианты записи и применения. [docs/VALIDATION.md](docs/VALIDATION.md) содержит результаты предыдущих проверок.

**Ещё не проверено.** При составлении карты приложение не запускалось. Приёмка на целевом Linux с реальной конфигурацией отмечена в README как отдельный этап.
