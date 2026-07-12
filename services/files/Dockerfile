# syntax=docker/dockerfile:1.7
FROM golang:1.25-alpine AS build
WORKDIR /src

COPY go.work go.work.sum ./
COPY pkg/go.mod pkg/go.sum ./pkg/
COPY contracts/gen/go/go.mod contracts/gen/go/go.sum ./contracts/gen/go/
COPY services/files/go.mod services/files/go.sum ./services/files/
RUN cd services/files && GOWORK=off go mod download

COPY pkg ./pkg
COPY contracts/gen/go ./contracts/gen/go
COPY services/files ./services/files
RUN cd services/files && GOWORK=off CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/files ./cmd/files

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/files /app/files
EXPOSE 8086 9086
USER nonroot:nonroot
ENTRYPOINT ["/app/files"]
