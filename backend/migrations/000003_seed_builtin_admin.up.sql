-- 000003_seed_builtin_admin.up.sql
-- 内置管理员种子（对齐 PRD §1.3：系统默认内置 admin / 首次登录强制改密 / 默认拥有全部权限）。
--
-- 安全说明：
--   * 落库值为 bcrypt(cost=10) 哈希，不存储明文；初始口令仅通过交付资料（缺省账号清单）下发，
--     不写入源码/二进制（DES-S-003 / TEST-S-003：`strings <bin>` 不得出现明文口令）。
--   * 迁移 SQL 会随 go:embed 进入二进制，故此处不得出现任何明文口令/密钥。
--   * is_builtin=TRUE 触发应用层隐式全权限 + 不可删除/停用（INV-10），不依赖 role 表。
--   * must_change_password=TRUE：首次登录仅可改密/登出/查询自身（DES-S-003）。
INSERT INTO admin (username, name, phone, email, description, password_hash,
                   must_change_password, status, is_builtin)
SELECT 'admin', '内置管理员', '', '', '系统内置管理员（默认全权限，首登强制改密）',
       '$2a$10$7CmQgZbwoq3SUa91tvP4yOQJEDVqZBuCd7h7HDOdCWwrVYqYqBSrq',
       TRUE, 'enabled', TRUE
WHERE NOT EXISTS (
    SELECT 1 FROM admin WHERE username = 'admin' AND deleted_at IS NULL
);
