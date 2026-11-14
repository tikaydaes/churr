# Builds the three static binaries and ships each on a minimal base
# that has CA certificates and time zone data but no shell.
#
#   docker build -t churr .                         the server
#   docker build -t churr-hub --target hub .        the hub
#   docker build -t churr-relay --target relay .    the push relay
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X github.com/tikaydaes/churr/internal/version.Version=${VERSION}" \
    -o /out/ ./cmd/churr ./cmd/churr-hub ./cmd/churr-relay

FROM gcr.io/distroless/static-debian12:nonroot AS hub
COPY --from=build /out/churr-hub /churr-hub
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/churr-hub"]
CMD ["serve", "-data", "/data", "-behind-proxy", "-http", ":8080"]

FROM gcr.io/distroless/static-debian12:nonroot AS relay
COPY --from=build /out/churr-relay /churr-relay
EXPOSE 8080
ENTRYPOINT ["/churr-relay"]
CMD ["serve", "-behind-proxy", "-http", ":8080"]

# Last, so a plain "docker build ." makes the server.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/churr /churr
VOLUME /data
EXPOSE 8080 24877/udp 24877/tcp
ENTRYPOINT ["/churr"]
CMD ["serve", "-data", "/data", "-behind-proxy", "-http", ":8080"]
