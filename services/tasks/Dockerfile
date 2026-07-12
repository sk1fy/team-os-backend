# syntax=docker/dockerfile:1.7
FROM golang:1.25-alpine AS build
WORKDIR /src

COPY go.work go.work.sum ./
COPY pkg/go.mod pkg/go.sum ./pkg/
COPY contracts/gen/go/go.mod contracts/gen/go/go.sum ./contracts/gen/go/
COPY services/tasks/go.mod services/tasks/go.sum ./services/tasks/
RUN cd services/tasks && GOWORK=off go mod download

COPY pkg ./pkg
COPY contracts/gen/go ./contracts/gen/go
COPY services/tasks ./services/tasks
RUN cd services/tasks && GOWORK=off CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/tasks ./cmd/tasks

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/tasks /app/tasks
EXPOSE 8083 9083
USER nonroot:nonroot
ENTRYPOINT ["/app/tasks"]