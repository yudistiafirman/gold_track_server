# --- build stage ---
FROM golang:1.26-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate

# --- runtime stage ---
FROM alpine:3.20
RUN apk add --no-cache ca-certificates && \
    adduser -D -u 10001 appuser
WORKDIR /app

COPY --from=build /out/api ./api
COPY --from=build /out/migrate ./migrate
COPY migrations ./migrations

USER appuser
EXPOSE 8080
ENTRYPOINT ["./api"]
