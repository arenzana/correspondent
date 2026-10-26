# syntax=docker/dockerfile:1
FROM node:26-alpine AS ui
WORKDIR /src/ui
COPY ui/package.json ui/package-lock.json ./
RUN npm ci
COPY ui/ ./
RUN npm run check && npm run build

FROM golang:1.27-alpine AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /correspondent ./cmd/correspondent

# Distroless static: no shell, no package manager, non-root, CA roots and tzdata included.
FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=go /correspondent /correspondent
ENV CORRESPONDENT_DATA=/data CORRESPONDENT_ADDR=:8080
VOLUME /data
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/correspondent"]
CMD ["serve"]
