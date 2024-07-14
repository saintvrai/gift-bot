# Step 1: Build stage
FROM golang:1.22-alpine as builder
WORKDIR /gift

COPY go.mod .
COPY go.sum .

RUN go mod download

COPY . .

ENV TZ=Europe/Moscow

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o gift-backend ./cmd/main.go

# Step 2: Runtime stage
FROM alpine:latest

# Install CA certificates
RUN apk --no-cache add ca-certificates

# Set the timezone
ENV TZ=Europe/Moscow

# Copy only the binary from the build stage to the final image
COPY --from=builder /gift /

# Set the entry point for the container
ENTRYPOINT ["/gift-backend"]