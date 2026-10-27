-- 000003_seed_builtin_admin.down.sql
-- 回滚内置 admin 种子（仅删除内置账号，避免误删同名自建账号）。
DELETE FROM admin_role
WHERE admin_id IN (SELECT id FROM admin WHERE username = 'admin' AND is_builtin = TRUE);

DELETE FROM admin WHERE username = 'admin' AND is_builtin = TRUE;
