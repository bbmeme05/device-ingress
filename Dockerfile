ARG IMAGE_PLATFORM=linux/amd64

FROM --platform=$IMAGE_PLATFORM golang:1.24-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/device-ingress \
    ./cmd/device-ingress

FROM --platform=$IMAGE_PLATFORM gcr.io/distroless/cc-debian12:nonroot

COPY --from=builder /out/device-ingress /app/device-ingress

USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app/device-ingress"]
