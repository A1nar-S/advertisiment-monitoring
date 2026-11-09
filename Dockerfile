# One image recipe for every service: docker build --build-arg CMD=scraper .

# The dashboard (served by the api service); small, so every image carries it.
FROM oven/bun:1.3.14 AS web
WORKDIR /web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web/ ./
RUN bun run build

FROM golang:1.26 AS build
ARG CMD
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/service ./cmd/${CMD}

# distroless/static has CA certs (needed for ss.com and Discord over TLS), no shell.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/service /service
COPY --from=web /web/dist /web
ENV WEB_DIR=/web
ENTRYPOINT ["/service"]
