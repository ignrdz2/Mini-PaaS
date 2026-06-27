ALTER TABLE deployments DROP COLUMN IF EXISTS rolled_back_from;
DROP TABLE IF EXISTS deployment_logs;
