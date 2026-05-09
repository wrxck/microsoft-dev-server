FROM golang:1.22-alpine AS builder

WORKDIR /src
COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
    -o /out/microsoft-dev-server ./cmd/microsoft-dev-server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/microsoft-dev-server /usr/local/bin/microsoft-dev-server
EXPOSE 8080
ENV ADDR=0.0.0.0:8080
ENTRYPOINT ["/usr/local/bin/microsoft-dev-server"]
