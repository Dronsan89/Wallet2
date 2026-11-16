// Индексы коллекции notification.transfers.
// Сервис создаёт их же при старте; скрипт нужен, чтобы индексы существовали
// сразу после поднятия контейнера, ещё до запуска gw-notification.

const database = db.getSiblingDB('notification');

database.transfers.createIndex({ transaction_id: 1 }, { unique: true, name: 'uniq_transaction_id' });
database.transfers.createIndex({ user_id: 1, created_at: -1 }, { name: 'user_created' });