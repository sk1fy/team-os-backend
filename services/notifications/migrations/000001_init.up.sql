CREATE TABLE notifications (
  id uuid PRIMARY KEY,
  company_id uuid NOT NULL,
  user_id uuid NOT NULL,
  type text NOT NULL CHECK (type IN ('task_assigned','task_comment','task_due','article_published','article_ack_required','course_assigned','course_due','mention')),
  title text NOT NULL,
  body text,
  link text,
  read boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notifications_user_read_created_idx ON notifications (user_id, read, created_at DESC);
CREATE TABLE processed_events (event_id uuid PRIMARY KEY, company_id uuid NOT NULL, processed_at timestamptz NOT NULL DEFAULT now());
