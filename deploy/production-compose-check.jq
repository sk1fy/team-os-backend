(.services.files.environment.FILES_S3_SECURE == "false") and
(.services.files.environment.FILES_S3_PUBLIC_SECURE == "true") and
(.services.gateway.environment.GATEWAY_COOKIE_SECURE == "true") and
([.services.postgres.ports, .services.nats.ports, .services.company.ports,
  .services.kb.ports, .services.tasks.ports, .services.academy.ports,
  .services.notifications.ports, .services.files.ports] | all(length == 0)) and
(.services.gateway.ports[0].host_ip == "127.0.0.1") and
(.services.minio.ports[0].host_ip == "127.0.0.1")
