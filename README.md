# logsviewer

TUI-инструмент на Go для просмотра и tail’инга JSON-логов. Слева список последних событий с настраиваемыми полями, справа — полный JSON с поддержкой прокрутки.

## Быстрый старт

```bash
# Установит версию v0.1.0 в /usr/local/bin/logsviewer
curl -fsSL https://raw.githubusercontent.com/marcuzy/logsviewer/v0.1.0/install.sh | bash
```

- Для другой директории установки: `INSTALL_DIR=$HOME/.local/bin …`
- Для выбора версии: `VERSION=v0.2.0 …`

После установки проверьте, что бинарь доступен: `logsviewer --help`.

## Ручная установка

1. Скачайте бинарник с [релизов](https://github.com/marcuzy/logsviewer/releases):
   - `logsviewer-darwin-arm64` для macOS (Apple Silicon)
   - `logsviewer-linux-amd64` для Linux x64
2. Сделайте исполняемым и переместите в каталог из `$PATH`:
   ```bash
   chmod +x logsviewer-*
   sudo mv logsviewer-darwin-arm64 /usr/local/bin/logsviewer   # пример для macOS
   ```

## Конфигурация

По умолчанию `logsviewer` ищет `logsviewer.{yaml,json,toml}` в:

- текущем каталоге;
- `~/.config/logsviewer/`;
- `~/.logsviewer/`.

Пример `~/.config/logsviewer/logsviewer.yaml`:

```yaml
files:
  - /var/log/app.jsonl
tail_lines: 500
max_entries: 2000
timestamp_field: timestamp
message_field: message
extra_fields:
  - level
  - "@file"
```

CLI-флаги перекрывают конфигурацию (см. `logsviewer --help`).

## Горячие клавиши

- Стрелки, `PgUp/PgDn`: навигация по списку или прокрутка JSON (зависит от фокуса).
- `Tab` / `Shift+Tab`: переключение фокуса между списком и правой панелью.
- `/`: поиск; `Enter` — применить, `Esc` — сбросить.
- `n` / `N`: следующая / предыдущая совпадающая запись.
- `f`: переключение дополнительного поля в списке.
- `q` или `Ctrl+C`: выход.

## Процесс релиза

1. Собрать бинарники:
   ```bash
   mkdir -p dist
   GOOS=darwin GOARCH=arm64 go build -o dist/logsviewer-darwin-arm64 ./cmd/logsviewer
   GOOS=linux GOARCH=amd64 go build -o dist/logsviewer-linux-amd64 ./cmd/logsviewer
   ```
2. Подготовить SHA256:
   ```bash
   shasum -a 256 dist/logsviewer-darwin-arm64 dist/logsviewer-linux-amd64
   ```
3. Создать тег (пример `v0.1.0`) и пуш:
   ```bash
   git tag v0.1.0
   git push origin v0.1.0
   ```
4. На странице GitHub Releases добавить новый релиз, загрузив оба бинарника и указав чек-суммы.
5. Убедиться, что `install.sh` обновлён до актуальной версии и ссылка в README указывает на новый тег.

После публикации установка по `curl ... install.sh` автоматически подтянет соответствующие активы релиза.
