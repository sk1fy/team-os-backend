# syntax=docker/dockerfile:1.7
FROM golang:1.25-alpine AS build
WORKDIR /src

COPY go.work go.work.sum ./
COPY pkg/go.mod pkg/go.sum ./pkg/
COPY contracts/gen/go/go.mod contracts/gen/go/go.sum ./contracts/gen/go/
COPY services/academy/go.mod services/academy/go.sum ./services/academy/
RUN cd services/academy && GOWORK=off go mod download

COPY pkg ./pkg
COPY contracts/gen/go ./contracts/gen/go
COPY services/academy ./services/academy
RUN cd services/academy && GOWORK=off CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/academy ./cmd/academy

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/academy /app/academy
EXPOSE 8084 9084
USER nonroot:nonroot
ENTRYPOINT ["/app/academy"]
