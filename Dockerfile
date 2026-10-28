# One image recipe for every service: docker build --build-arg CMD=scraper .
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
COPY config /config
ENTRYPOINT ["/service"]
