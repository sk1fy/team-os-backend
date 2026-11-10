package application

import "context"

func (s *Service) AmoAdminSelfLogin(_ context.Context, _ AmoAdminSelfLoginInput) (AmoAdminSelfLoginResult, error) {
	return AmoAdminSelfLoginResult{}, coded(ErrorForbidden, ErrorCodeAmoBrowserEntryDisabled, "Вход по данным браузера отключён. Откройте TeamOS из виджета amoCRM")
}
