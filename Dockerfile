# syntax=docker/dockerfile:1.6

# ---- Stage 1: build the React frontend ----
FROM node:22-alpine AS web-builder

WORKDIR /web
COPY web/package.json web/pnpm-lock.yaml ./
RUN corepack enable && corepack prepare pnpm@10 --activate
RUN pnpm install --frozen-lockfile

COPY web/ ./
RUN pnpm build

# ---- Stage 2: build the Go binary with embedded SPA ----
FROM golang:1.25-alpine AS go-builder

# CA certs needed at runtime for api.anthropic.com / api.osv.dev
RUN apk add --no-cache ca-certificates

WORKDIR /src

# Cache deps separately for faster rebuilds
COPY go.mod go.sum ./
RUN go mod download

# Copy source + the frontend artifacts into the embed location
COPY . .
COPY --from=web-builder /web/dist ./internal/api/dist

# Build flags: static binary, no debug info, version stamped from build arg
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-s -w \
      -X main.version=${VERSION} \
      -X main.commit=${COMMIT} \
      -X main.buildDate=${BUILD_DATE}" \
    -o /out/assay ./cmd/assay

# ---- Stage 3: final scratch image ----
FROM scratch

# Copy CA certs for HTTPS calls to api.anthropic.com
COPY --from=go-builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy the binary
COPY --from=go-builder /out/assay /assay

# Copy LICENSE for compliance
COPY LICENSE /LICENSE

EXPOSE 7373

# Default to printing version when no command provided; users supply args like:
#   docker run --rm -v $PWD:/scan ghcr.io/chawdamrunal/assay scan /scan
ENTRYPOINT ["/assay"]
CMD ["version"]
