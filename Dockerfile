FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/ez-gen-api .

FROM alpine:3.21
RUN apk --no-cache add ca-certificates tzdata ffmpeg ttf-dejavu
WORKDIR /app
COPY --from=builder /bin/ez-gen-api .
EXPOSE 8080
CMD ["./ez-gen-api"]
