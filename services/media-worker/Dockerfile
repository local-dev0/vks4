# syntax=docker/dockerfile:1.7

# ---------- build ----------
FROM golang:1.23-bookworm AS build
WORKDIR /src

# go-gst требует CGO + dev-headers GStreamer
# Подменяем стандартное зеркало на mirror.yandex.ru — стабильнее из RU-сети.
RUN printf 'deb http://mirror.yandex.ru/debian bookworm main\ndeb http://mirror.yandex.ru/debian bookworm-updates main\ndeb http://mirror.yandex.ru/debian-security bookworm-security main\n' > /etc/apt/sources.list \
    && apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates pkg-config build-essential \
    libgstreamer1.0-dev \
    libgstreamer-plugins-base1.0-dev \
    && rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum* ./
COPY . .
RUN go mod tidy

ENV CGO_ENABLED=1 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/media-worker ./cmd/worker

# ---------- runtime ----------
FROM debian:bookworm-slim AS runtime
RUN printf 'deb http://mirror.yandex.ru/debian bookworm main\ndeb http://mirror.yandex.ru/debian bookworm-updates main\ndeb http://mirror.yandex.ru/debian-security bookworm-security main\n' > /etc/apt/sources.list \
    && apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    gstreamer1.0-tools \
    gstreamer1.0-plugins-base \
    gstreamer1.0-plugins-good \
    gstreamer1.0-plugins-bad \
    gstreamer1.0-plugins-ugly \
    gstreamer1.0-libav \
    libgstreamer1.0-0 \
    libgstreamer-plugins-base1.0-0 \
    gstreamer1.0-x \
    libpango-1.0-0 libpangocairo-1.0-0 libpangoft2-1.0-0 \
    fonts-dejavu-core \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=build /out/media-worker /app/bin/media-worker
RUN useradd -r -u 10001 -m -d /home/worker worker \
    && mkdir -p /data/recordings /home/worker/.cache/fontconfig \
    && chown -R worker:worker /data/recordings /home/worker
# HOME и XDG_CACHE_HOME нужны pango/fontconfig для cache'а; textoverlay без них падает с
# "Fontconfig error: No writable cache directories".
ENV HOME=/home/worker XDG_CACHE_HOME=/home/worker/.cache
USER worker
EXPOSE 9091 9100
ENTRYPOINT ["/app/bin/media-worker"]
