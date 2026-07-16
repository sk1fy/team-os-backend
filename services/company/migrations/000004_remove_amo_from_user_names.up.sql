UPDATE users
SET last_name = '', updated_at = now()
WHERE source = 'amo' AND last_name = 'amoCRM';
