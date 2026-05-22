# syntax=docker/dockerfile:1.7
FROM node:22-alpine AS build
WORKDIR /src
# npmmirror.com — китайский mirror npmjs, стабильнее с RU-сети.
# Также увеличенные retry timings для медленных/нестабильных соединений.
ENV NPM_CONFIG_REGISTRY=https://registry.npmmirror.com/ \
    NPM_CONFIG_FETCH_RETRIES=10 \
    NPM_CONFIG_FETCH_RETRY_MINTIMEOUT=20000 \
    NPM_CONFIG_FETCH_RETRY_MAXTIMEOUT=180000 \
    NPM_CONFIG_FETCH_TIMEOUT=600000
COPY package.json package-lock.json* ./
RUN npm install --no-audit --no-fund
COPY . .
# Если openapi-typescript ещё не сгенерирован — пытаемся; не падаем, если файла нет.
RUN npm run gen:openapi || true
RUN npm run build

FROM nginxinc/nginx-unprivileged:alpine AS runtime
COPY --from=build /src/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 8080
