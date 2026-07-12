# syntax=docker/dockerfile:1.7
FROM golang:1.25-alpine AS build
WORKDIR /src

COPY go.work go.work.sum ./
COPY pkg/go.mod pkg/go.sum ./pkg/
COPY contracts/gen/go/go.mod contracts/gen/go/go.sum ./contracts/gen/go/
COPY services/kb/go.mod services/kb/go.sum ./services/kb/
RUN cd services/kb && GOWORK=off go mod download

COPY pkg ./pkg
COPY contracts/gen/go ./contracts/gen/go
COPY services/kb ./services/kb
RUN cd services/kb && GOWORK=off CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/kb ./cmd/kb

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/kb /app/kb
EXPOSE 8082 9082
USER nonroot:nonroot
ENTRYPOINT ["/app/kb"]