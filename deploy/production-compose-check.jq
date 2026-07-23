(.services.files.environment.FILES_S3_SECURE == "false") and
(.services.files.environment.FILES_S3_PUBLIC_SECURE == "true") and
(.services.gateway.environment.GATEWAY_COOKIE_SECURE == "true") and
(.services.notifications.environment.NOTIFICATIONS_EMAIL_PROVIDER == "smtp") and
(.services.notifications.environment.NOTIFICATIONS_SMTP_HOST | length > 0) and
(.services.notifications.environment.NOTIFICATIONS_SMTP_FROM | length > 0) and
(.services.academy.environment.ACADEMY_EXTERNAL_TOKEN_SECRET != "development-academy-external-secret-change-me") and
(.services.academy.environment.ACADEMY_EXTERNAL_EMAIL_KEY != "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=") and
(.services.notifications.environment.NOTIFICATIONS_EXTERNAL_EMAIL_KEY == .services.academy.environment.ACADEMY_EXTERNAL_EMAIL_KEY) and
([.services.postgres.ports, .services.nats.ports, .services.company.ports,
  .services.kb.ports, .services.tasks.ports, .services.academy.ports,
  .services.notifications.ports, .services.files.ports] | all(length == 0)) and
(.services.gateway.ports[0].host_ip == "127.0.0.1") and
(.services.minio.ports[0].host_ip == "127.0.0.1")
