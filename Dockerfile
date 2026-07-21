FROM node:22.21.1-alpine3.21 AS ui_builder
ARG NODE_ENV=production
ENV NODE_ENV=$NODE_ENV
WORKDIR /ui
COPY ui/package.json ./
RUN npm install --include=dev
COPY ui .
RUN npm run build

# Stage 1: Build the Go binary
FROM golang:1.25-alpine3.22 AS go_builder

# Set the working directory
WORKDIR /app

# Copy the source code to the container
COPY . .

# Build the Go binary
RUN go build -o rds-scaler .

# Stage 2: Create the runtime image
FROM alpine:3.22 AS runner

# Set the working directory
WORKDIR /app

# Copy the binary from the build stage to the runtime stage
COPY --from=go_builder /app/rds-scaler .
COPY --from=ui_builder /ui/dist ./ui/build

# Install ca-certificates for SSL support (required for AWS SDK)
RUN apk add --no-cache ca-certificates

USER nobody
EXPOSE 8041

# Run the Go binary
CMD ["./rds-scaler"]
