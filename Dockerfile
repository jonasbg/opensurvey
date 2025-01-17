# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .

# Final stage
FROM scratch

WORKDIR /app

COPY --from=builder /app/main .
COPY --from=builder /app/views /app/views
COPY --from=builder /app/static /app/static

EXPOSE 8080

ENTRYPOINT ["/app/main"]