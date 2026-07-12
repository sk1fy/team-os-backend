FROM golang:1.25 AS build
WORKDIR /src
COPY . .
RUN cd services/notifications && GOWORK=off go build -o /out/notifications ./cmd/notifications
FROM gcr.io/distroless/base-debian12
COPY --from=build /out/notifications /notifications
ENTRYPOINT ["/notifications"]
