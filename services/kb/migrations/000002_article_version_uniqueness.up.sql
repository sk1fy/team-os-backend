CREATE UNIQUE INDEX article_versions_article_version_uidx
    ON article_versions (article_id, version);
