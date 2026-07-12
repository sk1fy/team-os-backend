package seed

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
)

// fixtureNamespace is stable for the lifetime of TeamOS fixture imports.
// Changing it would change every legacy (non-UUID) fixture identifier.
var fixtureNamespace = uuid.MustParse("7c29d63e-2954-5e23-9cba-7b950e8cc1a8")

type Dataset struct {
	Company     Company
	Users       []User
	Departments []Department
	Positions   []Position
	Invites     []Invite
}

type Company struct {
	ID        uuid.UUID
	Name      string
	LogoURL   *string
	OwnerID   uuid.UUID
	CreatedAt *time.Time
}

type User struct {
	ID                uuid.UUID
	CompanyID         uuid.UUID
	Email             string
	FirstName         string
	LastName          string
	AvatarURL         *string
	Phone             *string
	Role              string
	Status            string
	PositionID        *uuid.UUID
	BirthDate         *time.Time
	HiredAt           *time.Time
	VacationAllowance *int16
	CreatedAt         *time.Time
}

type Department struct {
	ID                   uuid.UUID
	CompanyID            uuid.UUID
	Name                 string
	ParentID             *uuid.UUID
	HeadUserID           *uuid.UUID
	ValuableFinalProduct *string
	Order                int32
}

type Position struct {
	ID                uuid.UUID
	CompanyID         uuid.UUID
	Name              string
	DepartmentID      uuid.UUID
	Level             int16
	Description       *string
	ArticleIDs        []uuid.UUID
	RequiredCourseIDs []uuid.UUID
}

type Invite struct {
	ID           uuid.UUID
	CompanyID    uuid.UUID
	Email        *string
	Token        string
	Role         string
	PositionID   *uuid.UUID
	DepartmentID *uuid.UUID
	InvitedByID  uuid.UUID
	Status       string
	ExpiresAt    time.Time
	CreatedAt    *time.Time
}

// MapID preserves valid UUID values and maps legacy IDs through a fixed UUIDv5
// namespace. References and entity IDs must always pass through this function.
func MapID(value string) (uuid.UUID, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return uuid.Nil, errors.New("пустой ID")
	}
	if parsed, err := uuid.Parse(normalized); err == nil {
		return parsed, nil
	}
	return uuid.NewSHA1(fixtureNamespace, []byte(normalized)), nil
}

// Normalize validates fixture relationships and rewrites every identifier to a
// PostgreSQL UUID before any database transaction is opened.
func Normalize(fixtures Fixtures, now time.Time) (Dataset, error) {
	now = now.UTC()
	companyID, err := MapID(fixtures.Company.ID)
	if err != nil {
		return Dataset{}, fmt.Errorf("company.id: %w", err)
	}
	companyName, err := required(fixtures.Company.Name, "company.name")
	if err != nil {
		return Dataset{}, err
	}
	createdAt, err := parseTimestamp(fixtures.Company.CreatedAt)
	if err != nil {
		return Dataset{}, fmt.Errorf("company.createdAt: %w", err)
	}
	ownerID, err := MapID(fixtures.CurrentUserID)
	if err != nil {
		return Dataset{}, fmt.Errorf("CURRENT_USER_ID: %w", err)
	}

	dataset := Dataset{
		Company: Company{
			ID:        companyID,
			Name:      companyName,
			LogoURL:   optionalText(fixtures.Company.LogoURL),
			OwnerID:   ownerID,
			CreatedAt: createdAt,
		},
		Users:       make([]User, 0, len(fixtures.Users)),
		Departments: make([]Department, 0, len(fixtures.Departments)),
		Positions:   make([]Position, 0, len(fixtures.Positions)),
		Invites:     make([]Invite, 0, len(fixtures.Invites)),
	}

	userIDs := make(map[uuid.UUID]string, len(fixtures.Users))
	emails := make(map[string]string, len(fixtures.Users))
	for index, raw := range fixtures.Users {
		label := fmt.Sprintf("users[%d]", index)
		id, err := MapID(raw.ID)
		if err != nil {
			return Dataset{}, fmt.Errorf("%s.id: %w", label, err)
		}
		if previous, exists := userIDs[id]; exists {
			return Dataset{}, fmt.Errorf("%s.id: дубликат UUID с пользователем %q", label, previous)
		}
		userIDs[id] = raw.ID

		email, err := normalizeEmail(raw.Email)
		if err != nil {
			return Dataset{}, fmt.Errorf("%s.email: %w", label, err)
		}
		if previous, exists := emails[email]; exists {
			return Dataset{}, fmt.Errorf("%s.email: дубликат с пользователем %q", label, previous)
		}
		emails[email] = raw.ID
		firstName, err := required(raw.FirstName, label+".firstName")
		if err != nil {
			return Dataset{}, err
		}
		lastName, err := required(raw.LastName, label+".lastName")
		if err != nil {
			return Dataset{}, err
		}
		if !validUserRole(raw.Role) {
			return Dataset{}, fmt.Errorf("%s.role: неизвестная роль %q", label, raw.Role)
		}
		if !validUserStatus(raw.Status) {
			return Dataset{}, fmt.Errorf("%s.status: неизвестный статус %q", label, raw.Status)
		}
		if len(raw.PositionIDs) > 1 {
			return Dataset{}, fmt.Errorf("%s.positionIds: у пользователя может быть не более одной должности", label)
		}
		var positionID *uuid.UUID
		if len(raw.PositionIDs) == 1 {
			mapped, mapErr := MapID(raw.PositionIDs[0])
			if mapErr != nil {
				return Dataset{}, fmt.Errorf("%s.positionIds[0]: %w", label, mapErr)
			}
			positionID = &mapped
		}
		birthDate, err := parseDate(raw.BirthDate)
		if err != nil {
			return Dataset{}, fmt.Errorf("%s.birthDate: %w", label, err)
		}
		hiredAt, err := parseDate(raw.HiredAt)
		if err != nil {
			return Dataset{}, fmt.Errorf("%s.hiredAt: %w", label, err)
		}
		createdAt, err := parseTimestamp(raw.CreatedAt)
		if err != nil {
			return Dataset{}, fmt.Errorf("%s.createdAt: %w", label, err)
		}
		if raw.VacationAllowance != nil && (*raw.VacationAllowance < 0 || *raw.VacationAllowance > 366) {
			return Dataset{}, fmt.Errorf("%s.vacationAllowance: ожидается значение от 0 до 366", label)
		}
		dataset.Users = append(dataset.Users, User{
			ID: id, CompanyID: companyID, Email: email, FirstName: firstName, LastName: lastName,
			AvatarURL: optionalText(raw.AvatarURL), Phone: optionalText(raw.Phone),
			Role: raw.Role, Status: raw.Status, PositionID: positionID,
			BirthDate: birthDate, HiredAt: hiredAt, VacationAllowance: raw.VacationAllowance,
			CreatedAt: createdAt,
		})
	}
	ownerRawID, ownerExists := userIDs[ownerID]
	if !ownerExists {
		return Dataset{}, fmt.Errorf("CURRENT_USER_ID %q не найден среди users", fixtures.CurrentUserID)
	}
	for _, user := range dataset.Users {
		if user.ID == ownerID && user.Role != "owner" {
			return Dataset{}, fmt.Errorf("CURRENT_USER_ID %q должен иметь роль owner", ownerRawID)
		}
	}

	departmentIDs := make(map[uuid.UUID]string, len(fixtures.Departments))
	parentByDepartment := make(map[uuid.UUID]*uuid.UUID, len(fixtures.Departments))
	siblingOrders := make(map[string]string, len(fixtures.Departments))
	for index, raw := range fixtures.Departments {
		label := fmt.Sprintf("departments[%d]", index)
		id, err := MapID(raw.ID)
		if err != nil {
			return Dataset{}, fmt.Errorf("%s.id: %w", label, err)
		}
		if previous, exists := departmentIDs[id]; exists {
			return Dataset{}, fmt.Errorf("%s.id: дубликат UUID с отделом %q", label, previous)
		}
		departmentIDs[id] = raw.ID
		name, err := required(raw.Name, label+".name")
		if err != nil {
			return Dataset{}, err
		}
		if raw.Order < 0 {
			return Dataset{}, fmt.Errorf("%s.order: ожидается неотрицательное значение", label)
		}
		parentID, err := optionalMappedID(raw.ParentID)
		if err != nil {
			return Dataset{}, fmt.Errorf("%s.parentId: %w", label, err)
		}
		if parentID != nil && *parentID == id {
			return Dataset{}, fmt.Errorf("%s.parentId: отдел не может быть родителем самому себе", label)
		}
		headUserID, err := optionalMappedID(raw.HeadUserID)
		if err != nil {
			return Dataset{}, fmt.Errorf("%s.headUserId: %w", label, err)
		}
		if headUserID != nil {
			if _, exists := userIDs[*headUserID]; !exists {
				return Dataset{}, fmt.Errorf("%s.headUserId: пользователь %q не найден", label, valueOf(raw.HeadUserID))
			}
		}
		parentByDepartment[id] = parentID
		parentKey := "root"
		if parentID != nil {
			parentKey = parentID.String()
		}
		orderKey := fmt.Sprintf("%s/%d", parentKey, raw.Order)
		if previous, exists := siblingOrders[orderKey]; exists {
			return Dataset{}, fmt.Errorf("%s.order: совпадает с отделом %q у того же родителя", label, previous)
		}
		siblingOrders[orderKey] = raw.ID
		dataset.Departments = append(dataset.Departments, Department{
			ID: id, CompanyID: companyID, Name: name, ParentID: parentID, HeadUserID: headUserID,
			ValuableFinalProduct: optionalText(raw.ValuableFinalProduct), Order: raw.Order,
		})
	}
	for id, parentID := range parentByDepartment {
		if parentID != nil {
			if _, exists := departmentIDs[*parentID]; !exists {
				return Dataset{}, fmt.Errorf("department %q: parentId не найден", departmentIDs[id])
			}
		}
	}
	if err := validateDepartmentCycles(departmentIDs, parentByDepartment); err != nil {
		return Dataset{}, err
	}

	positionIDs := make(map[uuid.UUID]string, len(fixtures.Positions))
	for index, raw := range fixtures.Positions {
		label := fmt.Sprintf("positions[%d]", index)
		id, err := MapID(raw.ID)
		if err != nil {
			return Dataset{}, fmt.Errorf("%s.id: %w", label, err)
		}
		if previous, exists := positionIDs[id]; exists {
			return Dataset{}, fmt.Errorf("%s.id: дубликат UUID с должностью %q", label, previous)
		}
		positionIDs[id] = raw.ID
		name, err := required(raw.Name, label+".name")
		if err != nil {
			return Dataset{}, err
		}
		departmentID, err := MapID(raw.DepartmentID)
		if err != nil {
			return Dataset{}, fmt.Errorf("%s.departmentId: %w", label, err)
		}
		if _, exists := departmentIDs[departmentID]; !exists {
			return Dataset{}, fmt.Errorf("%s.departmentId: отдел %q не найден", label, raw.DepartmentID)
		}
		level := int16(0)
		if raw.Level != nil {
			level = *raw.Level
		}
		if level < 0 || level > 4 {
			return Dataset{}, fmt.Errorf("%s.level: ожидается значение от 0 до 4", label)
		}
		articleIDs, err := mapIDList(raw.ArticleIDs)
		if err != nil {
			return Dataset{}, fmt.Errorf("%s.articleIds: %w", label, err)
		}
		courseIDs, err := mapIDList(raw.RequiredCourseIDs)
		if err != nil {
			return Dataset{}, fmt.Errorf("%s.requiredCourseIds: %w", label, err)
		}
		dataset.Positions = append(dataset.Positions, Position{
			ID: id, CompanyID: companyID, Name: name, DepartmentID: departmentID, Level: level,
			Description: optionalText(raw.Description), ArticleIDs: articleIDs, RequiredCourseIDs: courseIDs,
		})
	}
	for index, user := range dataset.Users {
		if user.PositionID != nil {
			if _, exists := positionIDs[*user.PositionID]; !exists {
				return Dataset{}, fmt.Errorf("users[%d].positionIds[0]: должность не найдена", index)
			}
		}
	}

	inviteIDs := make(map[uuid.UUID]string, len(fixtures.Invites))
	tokens := make(map[string]string, len(fixtures.Invites))
	for index, raw := range fixtures.Invites {
		label := fmt.Sprintf("invites[%d]", index)
		id, err := MapID(raw.ID)
		if err != nil {
			return Dataset{}, fmt.Errorf("%s.id: %w", label, err)
		}
		if previous, exists := inviteIDs[id]; exists {
			return Dataset{}, fmt.Errorf("%s.id: дубликат UUID с приглашением %q", label, previous)
		}
		inviteIDs[id] = raw.ID
		token, err := required(raw.Token, label+".token")
		if err != nil {
			return Dataset{}, err
		}
		if previous, exists := tokens[token]; exists {
			return Dataset{}, fmt.Errorf("%s.token: дубликат с приглашением %q", label, previous)
		}
		tokens[token] = raw.ID
		if !validUserRole(raw.Role) {
			return Dataset{}, fmt.Errorf("%s.role: неизвестная роль %q", label, raw.Role)
		}
		if !validInviteStatus(raw.Status) {
			return Dataset{}, fmt.Errorf("%s.status: неизвестный статус %q", label, raw.Status)
		}
		email := optionalText(raw.Email)
		if email != nil {
			normalized, normalizeErr := normalizeEmail(*email)
			if normalizeErr != nil {
				return Dataset{}, fmt.Errorf("%s.email: %w", label, normalizeErr)
			}
			email = &normalized
		}
		positionID, err := optionalMappedID(raw.PositionID)
		if err != nil {
			return Dataset{}, fmt.Errorf("%s.positionId: %w", label, err)
		}
		if positionID != nil {
			if _, exists := positionIDs[*positionID]; !exists {
				return Dataset{}, fmt.Errorf("%s.positionId: должность %q не найдена", label, valueOf(raw.PositionID))
			}
		}
		departmentID, err := optionalMappedID(raw.DepartmentID)
		if err != nil {
			return Dataset{}, fmt.Errorf("%s.departmentId: %w", label, err)
		}
		if departmentID != nil {
			if _, exists := departmentIDs[*departmentID]; !exists {
				return Dataset{}, fmt.Errorf("%s.departmentId: отдел %q не найден", label, valueOf(raw.DepartmentID))
			}
		}
		invitedByID, err := MapID(raw.InvitedByID)
		if err != nil {
			return Dataset{}, fmt.Errorf("%s.invitedById: %w", label, err)
		}
		if _, exists := userIDs[invitedByID]; !exists {
			return Dataset{}, fmt.Errorf("%s.invitedById: пользователь %q не найден", label, raw.InvitedByID)
		}
		createdAt, err := parseTimestamp(raw.CreatedAt)
		if err != nil {
			return Dataset{}, fmt.Errorf("%s.createdAt: %w", label, err)
		}
		expiresAt, err := parseTimestamp(raw.ExpiresAt)
		if err != nil {
			return Dataset{}, fmt.Errorf("%s.expiresAt: %w", label, err)
		}
		if expiresAt == nil {
			base := now
			if createdAt != nil {
				base = *createdAt
			}
			derived := base.Add(7 * 24 * time.Hour)
			if raw.Status == "expired" && !derived.Before(now) {
				derived = now.Add(-time.Second)
			}
			expiresAt = &derived
		}
		dataset.Invites = append(dataset.Invites, Invite{
			ID: id, CompanyID: companyID, Email: email, Token: token, Role: raw.Role,
			PositionID: positionID, DepartmentID: departmentID, InvitedByID: invitedByID,
			Status: raw.Status, ExpiresAt: *expiresAt, CreatedAt: createdAt,
		})
	}

	return dataset, nil
}

func required(value, field string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("%s: обязательное поле пусто", field)
	}
	return trimmed, nil
}

func optionalText(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizeEmail(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	address, err := mail.ParseAddress(normalized)
	if err != nil || address.Address != normalized {
		return "", errors.New("некорректный email")
	}
	return normalized, nil
}

func optionalMappedID(value *string) (*uuid.UUID, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil, nil
	}
	mapped, err := MapID(*value)
	if err != nil {
		return nil, err
	}
	return &mapped, nil
}

func mapIDList(values []string) ([]uuid.UUID, error) {
	mapped := make([]uuid.UUID, 0, len(values))
	for index, value := range values {
		id, err := MapID(value)
		if err != nil {
			return nil, fmt.Errorf("[%d]: %w", index, err)
		}
		mapped = append(mapped, id)
	}
	return mapped, nil
}

func parseTimestamp(value string) (*time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, trimmed)
	if err == nil {
		utc := parsed.UTC()
		return &utc, nil
	}
	parsed, err = time.Parse(time.DateOnly, trimmed)
	if err != nil {
		return nil, errors.New("ожидается ISO-8601 дата или дата-время")
	}
	return &parsed, nil
}

func parseDate(value string) (*time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}
	if len(trimmed) < len(time.DateOnly) {
		return nil, errors.New("ожидается дата YYYY-MM-DD")
	}
	parsed, err := time.Parse(time.DateOnly, trimmed[:len(time.DateOnly)])
	if err != nil {
		return nil, errors.New("ожидается дата YYYY-MM-DD")
	}
	return &parsed, nil
}

func validUserRole(value string) bool {
	switch value {
	case "owner", "admin", "employee", "partner":
		return true
	default:
		return false
	}
}

func validUserStatus(value string) bool {
	switch value {
	case "active", "invited", "deactivated":
		return true
	default:
		return false
	}
}

func validInviteStatus(value string) bool {
	switch value {
	case "pending", "accepted", "expired":
		return true
	default:
		return false
	}
}

func validateDepartmentCycles(
	names map[uuid.UUID]string,
	parents map[uuid.UUID]*uuid.UUID,
) error {
	const (
		unvisited = iota
		visiting
		visited
	)
	state := make(map[uuid.UUID]int, len(parents))
	var visit func(uuid.UUID) error
	visit = func(id uuid.UUID) error {
		switch state[id] {
		case visiting:
			return fmt.Errorf("department %q: цикл в parentId", names[id])
		case visited:
			return nil
		}
		state[id] = visiting
		if parent := parents[id]; parent != nil {
			if err := visit(*parent); err != nil {
				return err
			}
		}
		state[id] = visited
		return nil
	}
	for id := range parents {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func valueOf(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
