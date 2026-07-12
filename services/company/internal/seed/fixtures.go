package seed

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// Fixtures is the company-owned subset exported from the frontend fixtures.
// It intentionally mirrors the JSON contract instead of the database schema.
type Fixtures struct {
	Company       CompanyFixture
	Users         []UserFixture
	Departments   []DepartmentFixture
	Positions     []PositionFixture
	Invites       []InviteFixture
	CurrentUserID string
}

type CompanyFixture struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	LogoURL   *string `json:"logoUrl"`
	OwnerID   string  `json:"ownerId"`
	CreatedAt string  `json:"createdAt"`
}

type UserFixture struct {
	ID                string   `json:"id"`
	Email             string   `json:"email"`
	FirstName         string   `json:"firstName"`
	LastName          string   `json:"lastName"`
	AvatarURL         *string  `json:"avatarUrl"`
	Phone             *string  `json:"phone"`
	Role              string   `json:"role"`
	Status            string   `json:"status"`
	PositionIDs       []string `json:"positionIds"`
	BirthDate         string   `json:"birthDate"`
	HiredAt           string   `json:"hiredAt"`
	VacationAllowance *int16   `json:"vacationAllowance"`
	CreatedAt         string   `json:"createdAt"`
}

type DepartmentFixture struct {
	ID                   string  `json:"id"`
	Name                 string  `json:"name"`
	ParentID             *string `json:"parentId"`
	HeadUserID           *string `json:"headUserId"`
	ValuableFinalProduct *string `json:"valuableFinalProduct"`
	Order                int32   `json:"order"`
}

type PositionFixture struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	DepartmentID      string   `json:"departmentId"`
	Level             *int16   `json:"level"`
	Description       *string  `json:"description"`
	ArticleIDs        []string `json:"articleIds"`
	RequiredCourseIDs []string `json:"requiredCourseIds"`
}

type InviteFixture struct {
	ID           string  `json:"id"`
	Email        *string `json:"email"`
	Token        string  `json:"token"`
	Role         string  `json:"role"`
	PositionID   *string `json:"positionId"`
	DepartmentID *string `json:"departmentId"`
	InvitedByID  string  `json:"invitedById"`
	Status       string  `json:"status"`
	ExpiresAt    string  `json:"expiresAt"`
	CreatedAt    string  `json:"createdAt"`
}

type fixtureDocuments struct {
	company          json.RawMessage
	users            json.RawMessage
	departments      json.RawMessage
	positions        json.RawMessage
	invites          json.RawMessage
	metadata         json.RawMessage
	currentUser      json.RawMessage
	companyFound     bool
	usersFound       bool
	departmentsFound bool
	positionsFound   bool
	invitesFound     bool
}

// Load reads either the standard collection of company.json/users.json/... files
// or the same values embedded in manifest.json, fixtures.json, or seed.json.
// Entity files take precedence over an aggregate manifest when both are present.
func Load(directory string) (Fixtures, error) {
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return Fixtures{}, errors.New("директория фикстур не задана")
	}
	info, err := os.Stat(directory)
	if err != nil {
		return Fixtures{}, fmt.Errorf("открыть директорию фикстур: %w", err)
	}
	if !info.IsDir() {
		return Fixtures{}, fmt.Errorf("путь фикстур %q не является директорией", directory)
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		return Fixtures{}, fmt.Errorf("прочитать директорию фикстур: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	var documents fixtureDocuments
	// Aggregate documents are loaded first so explicit entity files can override them.
	for _, aggregate := range []bool{true, false} {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			kind := fixtureFileKind(entry.Name())
			if kind == "" || isAggregateKind(kind) != aggregate {
				continue
			}
			body, readErr := os.ReadFile(filepath.Join(directory, entry.Name()))
			if readErr != nil {
				return Fixtures{}, fmt.Errorf("прочитать %s: %w", entry.Name(), readErr)
			}
			if err := documents.add(kind, body); err != nil {
				return Fixtures{}, fmt.Errorf("разобрать %s: %w", entry.Name(), err)
			}
		}
	}

	missing := documents.missing()
	if len(missing) > 0 {
		return Fixtures{}, fmt.Errorf("не найдены обязательные фикстуры: %s", strings.Join(missing, ", "))
	}

	var fixtures Fixtures
	if err := decodeEntity(documents.company, []string{"company", "companies"}, &fixtures.Company); err != nil {
		return Fixtures{}, fmt.Errorf("company: %w", err)
	}
	if err := decodeEntity(documents.users, []string{"users", "user"}, &fixtures.Users); err != nil {
		return Fixtures{}, fmt.Errorf("users: %w", err)
	}
	if err := decodeEntity(documents.departments, []string{"departments", "department"}, &fixtures.Departments); err != nil {
		return Fixtures{}, fmt.Errorf("departments: %w", err)
	}
	if err := decodeEntity(documents.positions, []string{"positions", "position"}, &fixtures.Positions); err != nil {
		return Fixtures{}, fmt.Errorf("positions: %w", err)
	}
	if err := decodeEntity(documents.invites, []string{"invites", "invite"}, &fixtures.Invites); err != nil {
		return Fixtures{}, fmt.Errorf("invites: %w", err)
	}

	currentSource := documents.currentUser
	if len(currentSource) == 0 {
		currentSource = documents.metadata
	}
	if len(currentSource) > 0 {
		fixtures.CurrentUserID, err = decodeCurrentUserID(currentSource)
		if err != nil {
			return Fixtures{}, fmt.Errorf("metadata/current user: %w", err)
		}
	}
	if strings.TrimSpace(fixtures.CurrentUserID) == "" {
		fixtures.CurrentUserID = fixtures.Company.OwnerID
	}
	if strings.TrimSpace(fixtures.CurrentUserID) == "" {
		for _, user := range fixtures.Users {
			if user.Role != "owner" {
				continue
			}
			if fixtures.CurrentUserID != "" {
				return Fixtures{}, errors.New("CURRENT_USER_ID не задан и в фикстурах несколько владельцев")
			}
			fixtures.CurrentUserID = user.ID
		}
	}
	if strings.TrimSpace(fixtures.CurrentUserID) == "" {
		return Fixtures{}, errors.New("CURRENT_USER_ID не найден в metadata и company.ownerId не задан")
	}
	return fixtures, nil
}

func (d *fixtureDocuments) add(kind string, body []byte) error {
	if len(strings.TrimSpace(string(body))) == 0 {
		return errors.New("пустой файл")
	}
	if isAggregateKind(kind) {
		if !json.Valid(body) {
			return errors.New("некорректный JSON")
		}
		d.addManifest(body)
		return nil
	}

	switch kind {
	case "company":
		d.company, d.companyFound = append(json.RawMessage(nil), body...), true
	case "users":
		d.users, d.usersFound = append(json.RawMessage(nil), body...), true
	case "departments":
		d.departments, d.departmentsFound = append(json.RawMessage(nil), body...), true
	case "positions":
		d.positions, d.positionsFound = append(json.RawMessage(nil), body...), true
	case "invites":
		d.invites, d.invitesFound = append(json.RawMessage(nil), body...), true
	case "metadata":
		d.metadata = append(json.RawMessage(nil), body...)
	case "currentuser":
		trimmed := strings.TrimSpace(string(body))
		if json.Valid(body) {
			d.currentUser = append(json.RawMessage(nil), body...)
		} else {
			encoded, _ := json.Marshal(trimmed)
			d.currentUser = encoded
		}
	}
	return nil
}

func (d *fixtureDocuments) addManifest(body []byte) {
	if value, ok := findWrappedValue(body, "company", "companies"); ok && !d.companyFound {
		d.company, d.companyFound = value, true
	}
	if value, ok := findWrappedValue(body, "users", "user"); ok && !d.usersFound {
		d.users, d.usersFound = value, true
	}
	if value, ok := findWrappedValue(body, "departments", "department"); ok && !d.departmentsFound {
		d.departments, d.departmentsFound = value, true
	}
	if value, ok := findWrappedValue(body, "positions", "position"); ok && !d.positionsFound {
		d.positions, d.positionsFound = value, true
	}
	if value, ok := findWrappedValue(body, "invites", "invite"); ok && !d.invitesFound {
		d.invites, d.invitesFound = value, true
	}
	if value, ok := findWrappedValue(body, "metadata", "meta"); ok && len(d.metadata) == 0 {
		d.metadata = value
	}
	if value, ok := findWrappedValue(body, "currentUserId", "CURRENT_USER_ID", "currentUser"); ok && len(d.currentUser) == 0 {
		d.currentUser = value
	}
}

func (d fixtureDocuments) missing() []string {
	missing := make([]string, 0, 5)
	if !d.companyFound {
		missing = append(missing, "company")
	}
	if !d.usersFound {
		missing = append(missing, "users")
	}
	if !d.departmentsFound {
		missing = append(missing, "departments")
	}
	if !d.positionsFound {
		missing = append(missing, "positions")
	}
	if !d.invitesFound {
		missing = append(missing, "invites")
	}
	return missing
}

func decodeEntity(raw json.RawMessage, keys []string, target any) error {
	value := unwrapEntity(raw, keys, 0)
	if len(value) == 0 || string(value) == "null" {
		return errors.New("пустое значение")
	}
	unmarshalErr := json.Unmarshal(value, target)
	if unmarshalErr == nil {
		return nil
	}

	// A companies.json export containing exactly one company is also accepted.
	if _, ok := target.(*CompanyFixture); ok {
		var companies []CompanyFixture
		if err := json.Unmarshal(value, &companies); err == nil && len(companies) == 1 {
			*target.(*CompanyFixture) = companies[0]
			return nil
		}
	}
	return fmt.Errorf("JSON не соответствует ожидаемой структуре: %w", unmarshalErr)
}

func unwrapEntity(raw json.RawMessage, keys []string, depth int) json.RawMessage {
	if depth > 8 || len(raw) == 0 {
		return raw
	}
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "[") {
		return raw
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return raw
	}
	for _, key := range keys {
		if value, ok := objectValue(object, key); ok {
			return unwrapEntity(value, keys, depth+1)
		}
	}
	for _, wrapper := range []string{"data", "items", "fixtures", "manifest", "payload", "seed"} {
		if value, ok := objectValue(object, wrapper); ok {
			return unwrapEntity(value, keys, depth+1)
		}
	}
	return raw
}

func findWrappedValue(raw json.RawMessage, keys ...string) (json.RawMessage, bool) {
	return findWrappedValueAtDepth(raw, keys, 0)
}

func findWrappedValueAtDepth(raw json.RawMessage, keys []string, depth int) (json.RawMessage, bool) {
	if depth > 8 {
		return nil, false
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return nil, false
	}
	for _, key := range keys {
		if value, ok := objectValue(object, key); ok {
			return value, true
		}
	}
	for _, wrapper := range []string{"data", "fixtures", "manifest", "payload", "seed"} {
		if value, ok := objectValue(object, wrapper); ok {
			if found, ok := findWrappedValueAtDepth(value, keys, depth+1); ok {
				return found, true
			}
		}
	}
	return nil, false
}

func decodeCurrentUserID(raw json.RawMessage) (string, error) {
	var direct string
	if json.Unmarshal(raw, &direct) == nil && strings.TrimSpace(direct) != "" {
		return strings.TrimSpace(direct), nil
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return "", errors.New("ожидается JSON-строка или объект metadata")
	}
	for _, key := range []string{"currentUserId", "CURRENT_USER_ID", "ownerId"} {
		if value, ok := objectValue(object, key); ok {
			if id, err := decodeCurrentUserID(value); err == nil {
				return id, nil
			}
		}
	}
	if value, ok := objectValue(object, "currentUser"); ok {
		var userObject map[string]json.RawMessage
		if json.Unmarshal(value, &userObject) == nil {
			if idValue, found := objectValue(userObject, "id"); found {
				return decodeCurrentUserID(idValue)
			}
		}
		return decodeCurrentUserID(value)
	}
	// current-user.json may contain the complete current user rather than only
	// the ID. Keep this fallback after the explicit metadata keys above.
	if value, ok := objectValue(object, "id"); ok {
		if id, err := decodeCurrentUserID(value); err == nil {
			return id, nil
		}
	}
	for _, wrapper := range []string{"metadata", "meta", "data", "fixtures", "manifest", "payload", "seed"} {
		if value, ok := objectValue(object, wrapper); ok {
			if id, err := decodeCurrentUserID(value); err == nil {
				return id, nil
			}
		}
	}
	return "", errors.New("поле currentUserId/CURRENT_USER_ID не найдено")
}

func objectValue(object map[string]json.RawMessage, name string) (json.RawMessage, bool) {
	wanted := canonicalName(name)
	for key, value := range object {
		if canonicalName(key) == wanted {
			return value, true
		}
	}
	return nil, false
}

func fixtureFileKind(name string) string {
	extension := strings.ToLower(filepath.Ext(name))
	if extension != ".json" && extension != ".txt" && extension != "" {
		return ""
	}
	base := canonicalName(strings.TrimSuffix(name, filepath.Ext(name)))
	switch base {
	case "company", "companies":
		return "company"
	case "user", "users":
		return "users"
	case "department", "departments":
		return "departments"
	case "position", "positions":
		return "positions"
	case "invite", "invites":
		return "invites"
	case "metadata", "meta":
		return "metadata"
	case "currentuser", "currentuserid":
		return "currentuser"
	case "manifest", "fixtures", "seed", "export", "allfixtures":
		return "manifest"
	default:
		return ""
	}
}

func isAggregateKind(kind string) bool {
	return kind == "manifest"
}

func canonicalName(value string) string {
	return strings.Map(func(character rune) rune {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			return unicode.ToLower(character)
		}
		return -1
	}, value)
}
