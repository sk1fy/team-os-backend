package application

import "context"

func (s *Service) BootstrapAmoCompany(_ context.Context, _ AmoCompanyBootstrapInput) (AmoCompanyBootstrapResult, error) {
	return AmoCompanyBootstrapResult{}, coded(ErrorForbidden, ErrorCodeAmoBrowserEntryDisabled, "Вход по данным браузера отключён. Откройте TeamOS из виджета amoCRM")
}
