-- Инициализация Postgres для всего стека.
-- Контейнер создаёт только базу POSTGRES_DB (wallet); вторую базу для сервиса
-- курсов и отдельного пользователя заводим здесь. Скрипт выполняется только
-- при первом создании тома pgdata.

CREATE USER exchanger WITH PASSWORD 'exchanger';

CREATE DATABASE exchanger OWNER exchanger;