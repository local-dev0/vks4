# syntax=docker/dockerfile:1.7
FROM golang:1.23-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum* ./
COPY . .
RUN go mod tidy
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/sip-gateway ./cmd/bridge

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
WORKDIR /app
COPY --from=build /out/sip-gateway /app/bin/sip-gateway
USER nonroot:nonroot
EXPOSE 9094 9100
ENTRYPOINT ["/app/bin/sip-gateway"]
