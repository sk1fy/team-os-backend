# Наблюдаемость TeamOS

Каталог предназначен для compose-профиля `observability`: Prometheus, Grafana, Loki,
Promtail, Tempo и OpenTelemetry Collector. Grafana автоматически получает источники данных и
дашборд «TeamOS — обзор».

Сервисы должны отдавать Prometheus-метрики на `/metrics` и отправлять OTLP в
`http://otel-collector:4318` (или gRPC на `otel-collector:4317`). Для алертов используются:

- counter `teamos_http_requests_total{method,path,status}` и histogram
  `teamos_http_request_duration_seconds`; label `service_name` добавляет Prometheus из target;
- gauge `teamos_outbox_oldest_pending_age_seconds{service_name}`;
- gauge `teamos_consumer_lag_messages{service_name,consumer}`;
- gauge `teamos_dlq_messages{service_name,subject}`.

Promtail требует read-only mounts `/var/run/docker.sock:/var/run/docker.sock` и
`/var/lib/docker/containers:/var/lib/docker/containers:ro`. Для production вместо Docker service
discovery следует читать journald и ограничить доступ агента логов.
