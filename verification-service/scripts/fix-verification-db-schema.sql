-- 修复 AutoMigrate 1170：旧表 tx_id 为 TEXT/LONGTEXT 带索引导致失败
-- 执行后重启 verification-service，由 GORM 按新模型重建表

USE stablepay_verification_db;
DROP TABLE IF EXISTS purchase_records;
DROP TABLE IF EXISTS x_verifications;
