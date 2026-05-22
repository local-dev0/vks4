# syntax=docker/dockerfile:1.7
FROM golang:1.23-alpine AS build
WORKDIR /src

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum* ./
COPY . .
RUN go mod tidy

ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
RUN go build -trimpath -ldflags="-s -w" -o /out/control-plane ./cmd/server

# ---------- runtime ----------
FROM gcr.io/distroless/static-debian12:nonroot AS runtime
WORKDIR /app
COPY --from=build /out/control-plane /app/bin/control-plane
COPY migrations /app/migrations
USER nonroot:nonroot
EXPOSE 8080 9100 9093
ENTRYPOINT ["/app/bin/control-plane"]
CMD ["serve"]
