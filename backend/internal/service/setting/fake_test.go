package setting

import (
	"context"
	"sort"
	"strings"
	"time"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// fakeRepo 内存设置仓储：实现 AdminRepo + RoleRepo（无真实 DB 依赖）。
type fakeRepo struct {
	admins    map[int64]*domain.Admin
	roles     map[int64]*domain.Role
	bindings  map[int64][]int64 // admin_id → role_ids
	nextAdmin int64
	nextRole  int64
	nextPerm  int64

	createAdminCalls int
	createdAdmin     *domain.Admin
	updateAdminCalls int
	statusCalls      map[int64]string
	deletedAdmins    []int64
	updatePwdCalls   int
	updatePwdHash    string
	updatePwdMust    bool
	bindCalls        int
	boundRoles       []int64
	deletedRoles     []int64
	updateRoleCalls  int
	lastRolePerms    []domain.RolePermission
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		admins:      map[int64]*domain.Admin{},
		roles:       map[int64]*domain.Role{},
		bindings:    map[int64][]int64{},
		nextAdmin:   100,
		nextRole:    200,
		nextPerm:    300,
		statusCalls: map[int64]string{},
	}
}

func (f *fakeRepo) ListAdmins(_ context.Context, keyword string, page, pageSize int) ([]domain.Admin, int64, error) {
	var all []domain.Admin
	for _, a := range f.admins {
		if keyword != "" && !strings.Contains(a.Username, keyword) && !strings.Contains(a.Name, keyword) {
			continue
		}
		all = append(all, *a)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	total := int64(len(all))
	start := (page - 1) * pageSize
	if start >= len(all) {
		return []domain.Admin{}, total, nil
	}
	end := start + pageSize
	if end > len(all) {
		end = len(all)
	}
	return all[start:end], total, nil
}

func (f *fakeRepo) GetAdminByID(_ context.Context, adminID int64) (*domain.Admin, error) {
	a, ok := f.admins[adminID]
	if !ok {
		return nil, errcode.ErrNotFound
	}
	cp := *a
	return &cp, nil
}

func (f *fakeRepo) CreateAdmin(_ context.Context, admin *domain.Admin) error {
	f.createAdminCalls++
	for _, a := range f.admins {
		if a.Username == admin.Username {
			return errcode.ErrConflict
		}
	}
	f.nextAdmin++
	admin.ID = f.nextAdmin
	admin.Version = 1
	admin.CreatedAt = time.Now()
	admin.UpdatedAt = admin.CreatedAt
	f.admins[admin.ID] = admin
	f.createdAdmin = admin
	return nil
}

func (f *fakeRepo) UpdateAdmin(_ context.Context, adminID int64, name, phone, email, description string, _ int64) error {
	a, ok := f.admins[adminID]
	if !ok {
		return errcode.ErrNotFound
	}
	f.updateAdminCalls++
	a.Name, a.Phone, a.Email, a.Description = name, phone, email, description
	return nil
}

func (f *fakeRepo) UpdateAdminStatus(_ context.Context, adminID int64, status string, _ int64) error {
	a, ok := f.admins[adminID]
	if !ok {
		return errcode.ErrNotFound
	}
	a.Status = status
	f.statusCalls[adminID] = status
	return nil
}

func (f *fakeRepo) UpdatePassword(_ context.Context, adminID int64, passwordHash string, mustChange bool) error {
	a, ok := f.admins[adminID]
	if !ok {
		return errcode.ErrNotFound
	}
	f.updatePwdCalls++
	f.updatePwdHash = passwordHash
	f.updatePwdMust = mustChange
	a.PasswordHash = passwordHash
	a.MustChangePassword = mustChange
	return nil
}

func (f *fakeRepo) SoftDeleteAdmin(_ context.Context, adminID int64) error {
	if _, ok := f.admins[adminID]; !ok {
		return errcode.ErrNotFound
	}
	f.deletedAdmins = append(f.deletedAdmins, adminID)
	delete(f.admins, adminID)
	return nil
}

func (f *fakeRepo) LoadRolesByAdmin(_ context.Context, adminID int64) ([]domain.Role, error) {
	out := make([]domain.Role, 0)
	for _, id := range f.bindings[adminID] {
		if r, ok := f.roles[id]; ok {
			cp := *r
			out = append(out, cp)
		}
	}
	return out, nil
}

func (f *fakeRepo) RolesByAdminIDs(_ context.Context, adminIDs []int64) (map[int64][]domain.Role, error) {
	out := make(map[int64][]domain.Role, len(adminIDs))
	for _, id := range adminIDs {
		out[id] = []domain.Role{}
		for _, rid := range f.bindings[id] {
			if r, ok := f.roles[rid]; ok {
				out[id] = append(out[id], *r)
			}
		}
	}
	return out, nil
}

func (f *fakeRepo) BindRoles(_ context.Context, adminID int64, roleIDs []int64) error {
	if _, ok := f.admins[adminID]; !ok {
		return errcode.ErrNotFound
	}
	f.bindCalls++
	f.boundRoles = append([]int64(nil), roleIDs...)
	f.bindings[adminID] = append([]int64(nil), roleIDs...)
	return nil
}

func (f *fakeRepo) ListRoles(_ context.Context) ([]domain.Role, error) {
	out := make([]domain.Role, 0, len(f.roles))
	for _, r := range f.roles {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (f *fakeRepo) GetRole(_ context.Context, roleID int64) (*domain.Role, error) {
	r, ok := f.roles[roleID]
	if !ok {
		return nil, errcode.ErrNotFound
	}
	cp := *r
	return &cp, nil
}

func (f *fakeRepo) CreateRole(_ context.Context, role *domain.Role) error {
	for _, r := range f.roles {
		if r.Name == role.Name {
			return errcode.ErrConflict
		}
	}
	f.nextRole++
	role.ID = f.nextRole
	role.Version = 1
	for i := range role.Permissions {
		f.nextPerm++
		role.Permissions[i].ID = f.nextPerm
		role.Permissions[i].RoleID = role.ID
	}
	f.roles[role.ID] = role
	f.lastRolePerms = append([]domain.RolePermission(nil), role.Permissions...)
	return nil
}

func (f *fakeRepo) UpdateRole(_ context.Context, role *domain.Role, _ int64) error {
	if _, ok := f.roles[role.ID]; !ok {
		return errcode.ErrNotFound
	}
	f.updateRoleCalls++
	cp := *role
	f.roles[role.ID] = &cp
	f.lastRolePerms = append([]domain.RolePermission(nil), role.Permissions...)
	return nil
}

func (f *fakeRepo) SoftDeleteRole(_ context.Context, roleID int64) error {
	if _, ok := f.roles[roleID]; !ok {
		return errcode.ErrNotFound
	}
	f.deletedRoles = append(f.deletedRoles, roleID)
	delete(f.roles, roleID)
	return nil
}

func (f *fakeRepo) CountAdminsByRole(_ context.Context, roleID int64) (int64, error) {
	var n int64
	for _, ids := range f.bindings {
		for _, id := range ids {
			if id == roleID {
				n++
			}
		}
	}
	return n, nil
}

func (f *fakeRepo) AdminIDsByRole(_ context.Context, roleID int64) ([]int64, error) {
	var out []int64
	for adminID, ids := range f.bindings {
		for _, id := range ids {
			if id == roleID {
				out = append(out, adminID)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func (f *fakeRepo) CountRolesByIDs(_ context.Context, roleIDs []int64) (int64, error) {
	var n int64
	for _, id := range roleIDs {
		if _, ok := f.roles[id]; ok {
			n++
		}
	}
	return n, nil
}

// fakeInvalidator 记录权限缓存失效调用。
type fakeInvalidator struct{ ids []int64 }

func (f *fakeInvalidator) InvalidatePermCache(adminID int64) {
	f.ids = append(f.ids, adminID)
}

func (f *fakeInvalidator) contains(id int64) bool {
	for _, v := range f.ids {
		if v == id {
			return true
		}
	}
	return false
}

func newTestService() (*Service, *fakeRepo, *fakeInvalidator) {
	repo := newFakeRepo()
	inv := &fakeInvalidator{}
	return New(repo, inv), repo, inv
}

// addAdmin 向假仓储写入管理员（便于构造内置 admin/引用关系）。
func addAdmin(repo *fakeRepo, id int64, username string, builtin bool) *domain.Admin {
	a := &domain.Admin{
		ID: id, Username: username, Name: username, Status: domain.AdminStatusEnabled,
		IsBuiltin: builtin, Version: 1,
	}
	repo.admins[id] = a
	return a
}

// addRole 向假仓储写入角色。
func addRole(repo *fakeRepo, id int64, name string) *domain.Role {
	r := &domain.Role{ID: id, Name: name, Version: 1}
	repo.roles[id] = r
	return r
}
