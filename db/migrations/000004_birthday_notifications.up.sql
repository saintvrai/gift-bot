CREATE TABLE birthday_notifications (
    id SERIAL PRIMARY KEY,
    admin_telegram_id BIGINT NOT NULL,
    user_telegram_id BIGINT NOT NULL,
    notify_date DATE NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX birthday_notifications_unique
    ON birthday_notifications (admin_telegram_id, user_telegram_id, notify_date);

CREATE INDEX birthday_notifications_notify_date_idx
    ON birthday_notifications (notify_date);
