# syntax=docker/dockerfile:1.7
FROM golang:1.26-alpine AS build
WORKDIR /src

COPY go.work go.work.sum ./
COPY pkg/go.mod pkg/go.sum ./pkg/
COPY contracts/gen/go/go.mod contracts/gen/go/go.sum ./contracts/gen/go/
COPY services/company/go.mod services/company/go.sum ./services/company/
RUN cd services/company && GOWORK=off go mod download

COPY pkg ./pkg
COPY contracts/gen/go ./contracts/gen/go
COPY services/company ./services/company
RUN cd services/company && GOWORK=off CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/company ./cmd/company

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/company /app/company
EXPOSE 8081 9081
USER nonroot:nonroot
ENTRYPOINT ["/app/company"]
