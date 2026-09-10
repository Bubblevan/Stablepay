-- Local deterministic-plane prerequisites. Services own their tables and run
-- GORM migrations; this file only ensures every service database exists before
-- a service attempts to connect.
CREATE DATABASE IF NOT EXISTS `stablepay_payment_db`
  DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE DATABASE IF NOT EXISTS `stablepay_verification_db`
  DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE DATABASE IF NOT EXISTS `stablepay_query_db`
  DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
