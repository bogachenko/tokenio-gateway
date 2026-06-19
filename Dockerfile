FROM golang:1.25-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/tokenio-gateway ./cmd/gateway

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=build /out/tokenio-gateway /app/tokenio-gateway
COPY --from=build /src/db /app/db

EXPOSE 8080

ENTRYPOINT ["/app/tokenio-gateway"]
