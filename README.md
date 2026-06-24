# ya2spotify

`ya2spotify` - локальное Go-приложение для переноса плейлиста из Яндекс Музыки в Spotify. Оно поднимает небольшой web UI, авторизует пользователя через Spotify OAuth, импортирует треки Яндекс Музыки по ссылке на плейлист, ищет соответствия в Spotify и создает новый Spotify-плейлист из найденных треков.

## Установка

Нужен Go 1.25.0 или совместимая версия toolchain.

```bash
go mod download
```

## Запуск

```bash
go run . -p 3500 -id <spotify_client_id> -secret <spotify_client_secret>
```

После запуска открой локальный интерфейс:

```text
http://127.0.0.1:3500/
```

## Конфигурация

Приложение принимает параметры командной строки:

| Флаг | Значение по умолчанию | Описание |
| --- | --- | --- |
| `-p` | `3500` | Порт локального web-сервера. |
| `-id` | встроенный client ID | Spotify Client ID. |
| `-secret` | встроенный client secret | Spotify Client Secret. |

Для собственного Spotify-приложения укажи Redirect URI:

```text
http://127.0.0.1:<port>/callback
```

## Структура репозитория

```text
.
├── main.go                  # точка входа и запуск локального сервера
├── spotifyconnect/          # OAuth, web UI, поиск и создание плейлистов Spotify
├── yandexmusic/             # загрузка метаданных и треков из Яндекс Музыки
├── go.mod
├── go.sum
└── .golangci.yml
```

## Примеры использования

1. Запусти приложение:

```bash
go run . -p 3500 -id <spotify_client_id> -secret <spotify_client_secret>
```

2. Перейди на `http://127.0.0.1:3500/` и нажми `login`.
3. После авторизации открой импорт плейлиста и вставь ссылку Яндекс Музыки вида:

```text
https://music.yandex.ru/users/<owner>/playlists/<playlist_id>
```

4. Нажми `Search on Spotify`, дождись завершения поиска и создай плейлист через `Add playlist to Spotify`.

## Тесты и линтеры

Форматирование и автоисправления:

```bash
golangci-lint fmt
GOTOOLCHAIN=go1.25.0 golangci-lint run --fix
```

Go-тесты:

```bash
GOTOOLCHAIN=go1.25.0 go test ./...
```
