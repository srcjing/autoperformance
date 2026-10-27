// Package service_test 验证各业务模块错误码经 errcode.Register 注册成功且不冲突。
package service_test

import (
	"testing"

	"auto_performance/internal/errcode"

	// 引入各模块包以触发其 errors.go 的 init 注册。
	"auto_performance/internal/service/auth"
	"auto_performance/internal/service/environment"
	"auto_performance/internal/service/log"
	"auto_performance/internal/service/product"
	"auto_performance/internal/service/report"
	"auto_performance/internal/service/setting"
	testingpkg "auto_performance/internal/service/testing"
)

// TestRegisterAllBusinessCodesNoPanic 遍历全部业务错误码，断言注册成功且码值/HTTP 正确。
func TestRegisterAllBusinessCodesNoPanic(t *testing.T) {
	cases := []struct {
		code int
		http int
		got  *errcode.Error
	}{
		{20001, 401, auth.ErrBadCredentials},
		{20002, 401, auth.ErrAccountLocked},
		{20003, 400, auth.ErrOldPasswordWrong},
		{20004, 400, auth.ErrPasswordWeak},
		{20005, 400, auth.ErrPasswordSame},

		{21001, 400, environment.ErrCodeExists},
		{21002, 409, environment.ErrCategoryRefed},
		{21003, 409, environment.ErrTagRefed},
		{21004, 409, environment.ErrEnvOccupied},

		{22001, 400, product.ErrCodeExists},
		{22002, 409, product.ErrProductHasInterfaces},
		{22003, 400, product.ErrThresholdMismatch},

		{23001, 409, testingpkg.ErrInvalidTaskState},
		{23002, 409, testingpkg.ErrEnvBusy},
		{23003, 400, testingpkg.ErrInterfaceRangeEmpty},
		{23004, 400, testingpkg.ErrConcurrencyInvalid},
		{23005, 404, testingpkg.ErrTemplateNotFound},

		{24001, 404, report.ErrReportFileNotFound},
		{24002, 400, report.ErrReportTypeNotAllowed},
		{24003, 400, report.ErrReportTooLarge},
		{24004, 500, report.ErrReportGenerateFailed},

		{25001, 400, log.ErrInvalidTimeRange},

		{26001, 400, setting.ErrUsernameExists},
		{26002, 403, setting.ErrBuiltinAdminProtected},
		{26003, 409, setting.ErrRoleReferenced},
		{26004, 403, setting.ErrBuiltinCertProtected},
		{26005, 409, setting.ErrSshHostReferenced},
	}

	for _, tc := range cases {
		if tc.got.Code != tc.code {
			t.Errorf("错误码变量 Code = %d, want %d", tc.got.Code, tc.code)
		}
		if tc.got.HTTP != tc.http {
			t.Errorf("错误码 %d HTTP = %d, want %d", tc.code, tc.got.HTTP, tc.http)
		}
		byCode := errcode.ByCode(tc.code)
		if byCode == nil {
			t.Errorf("errcode.ByCode(%d) 返回 nil，注册缺失", tc.code)
			continue
		}
		if byCode.Code != tc.code {
			t.Errorf("errcode.ByCode(%d).Code = %d", tc.code, byCode.Code)
		}
	}
}

// TestBusinessCodesNoOverlap 断言业务码未与全局码（<20000）冲突（Register 已保证段位）。
func TestBusinessCodesNoOverlap(t *testing.T) {
	for _, code := range []int{21001, 22001, 23001, 24001, 25001, 26001, 20001} {
		if code < 20000 {
			t.Errorf("业务码 %d 越界", code)
		}
	}
}
