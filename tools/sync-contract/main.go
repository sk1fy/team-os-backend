// sync-contract сверяет REST-вызовы фронтенда с contracts/openapi/teamos.yaml.
//
// Инструмент извлекает вызовы request()/publicRequest()/httpRequest()/openEventStream(),
// а также типизированных Academy-хелперов academyGet()/academyMutate() и
// externalGet()/externalMutate() из src/api фронтенда (FRONTEND_DIR). Затем он
// проверяет, что каждый путь и метод описан в OpenAPI-контракте. Завершается с
// ненулевым кодом, если фронтенд обращается к эндпоинту, которого нет в контракте.
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	specPath   = "contracts/openapi/teamos.yaml"
	apiPrefix  = "/api/v1"
	defaultDir = "../team-os"
)

type call struct {
	Method string
	Path   string // нормализованный: ${...} заменён на {param}, query отброшен
	File   string
	Line   int
}

func main() {
	frontendDir := strings.TrimSpace(os.Getenv("FRONTEND_DIR"))
	if frontendDir == "" {
		frontendDir = defaultDir
	}
	if _, err := os.Stat(frontendDir); err != nil {
		fatalf("FRONTEND_DIR недоступен: %v", err)
	}

	specMethods, err := loadSpec(specPath)
	if err != nil {
		fatalf("чтение контракта %s: %v", specPath, err)
	}

	calls, err := collectCalls(frontendDir)
	if err != nil {
		fatalf("разбор фронтенда: %v", err)
	}
	if len(calls) == 0 {
		fatalf("во фронтенде не найдено ни одного HTTP-вызова — проверьте FRONTEND_DIR")
	}

	var missing []string
	matched := map[string]struct{}{}
	for _, c := range calls {
		fullPath := apiPrefix + c.Path
		specKey, ok := matchSpec(fullPath, c.Method, specMethods)
		if !ok {
			missing = append(missing, fmt.Sprintf("%s:%d: %s %s", c.File, c.Line, c.Method, fullPath))
			continue
		}
		matched[specKey] = struct{}{}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		fmt.Fprintln(os.Stderr, "Вызовы фронтенда, отсутствующие в OpenAPI-контракте:")
		for _, m := range missing {
			fmt.Fprintln(os.Stderr, "  "+m)
		}
		os.Exit(1)
	}

	var unused []string
	for key := range specMethods {
		if _, ok := matched[key]; !ok {
			unused = append(unused, key)
		}
	}
	sort.Strings(unused)
	fmt.Printf("sync-contract: %d вызовов фронтенда покрыты контрактом\n", len(calls))
	if len(unused) > 0 {
		// Аддитивная эволюция допускает эндпоинты без потребителя — это не ошибка.
		fmt.Printf("Эндпоинты контракта без вызова из src/api (%d):\n", len(unused))
		for _, key := range unused {
			fmt.Println("  " + key)
		}
	}
}

// callPattern распознаёт вызов HTTP-хелпера с первой строкой-аргументом. Имя
// хелпера сохраняется отдельно: у *Get метод задан самой сигнатурой, а у
// *Mutate он обязан быть вторым строковым аргументом. Это не даёт ошибочно
// принять mutation с вычисляемым/неразобранным методом за GET.
var callPattern = regexp.MustCompile(
	"(?:\\b(publicRequest|request|httpRequest|openEventStream|fetch|academyGet|academyMutate|externalGet|externalMutate))" +
		"(?:<[^()]*>)?\\(\\s*(?:'([^']*)'|\"([^\"]*)\"|`([^`]*)`)",
)

var methodPattern = regexp.MustCompile(`["'](GET|POST|PUT|PATCH|DELETE)["']|method:\s*["'](GET|POST|PUT|PATCH|DELETE)["']`)

var mutateMethodPattern = regexp.MustCompile(`^\s*,\s*["'](POST|PUT|PATCH|DELETE)["']`)

func collectCalls(frontendDir string) ([]call, error) {
	frontendFiles, err := discoverFrontendFiles(frontendDir)
	if err != nil {
		return nil, err
	}
	var calls []call
	for _, rel := range frontendFiles {
		file := filepath.Join(frontendDir, rel)
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		content := string(data)
		for _, m := range callPattern.FindAllStringSubmatchIndex(content, -1) {
			helper := submatch(content, m, 1)
			raw := submatch(content, m, 2)
			if raw == "" {
				raw = submatch(content, m, 3)
			}
			if raw == "" {
				raw = submatch(content, m, 4)
			}
			raw = strings.TrimPrefix(raw, "${API_URL}")
			if !strings.HasPrefix(raw, "/") {
				continue
			}
			// Метод ищем в аргументах после пути: request(path, 'POST', …) либо
			// httpRequest/fetch(path, { method: 'POST', … }) — опции бывают многострочными.
			rest := content[m[1]:min(m[1]+500, len(content))]
			// Не заглядываем в аргументы следующего вызова.
			if next := callPattern.FindStringIndex(rest); next != nil {
				rest = rest[:next[0]]
			}
			method, methodErr := extractMethod(helper, rest)
			if methodErr != nil {
				line := 1 + strings.Count(content[:m[0]], "\n")
				return nil, fmt.Errorf("%s:%d: %w", rel, line, methodErr)
			}
			calls = append(calls, call{
				Method: method,
				Path:   normalizePath(raw),
				File:   rel,
				Line:   1 + strings.Count(content[:m[0]], "\n"),
			})
		}
	}
	return calls, nil
}

func extractMethod(helper, rest string) (string, error) {
	switch helper {
	case "academyGet", "externalGet":
		return "GET", nil
	case "academyMutate", "externalMutate":
		match := mutateMethodPattern.FindStringSubmatch(rest)
		if match == nil {
			return "", fmt.Errorf("для %s метод должен быть строковым литералом", helper)
		}
		return match[1], nil
	default:
		if match := methodPattern.FindStringSubmatch(rest); match != nil {
			if match[1] != "" {
				return match[1], nil
			}
			return match[2], nil
		}
		return "GET", nil
	}
}

// discoverFrontendFiles возвращает все runtime TypeScript-файлы src/api.
// Тесты и декларации исключаются: в них могут встречаться искусственные URL,
// которые не являются вызовами production-клиента.
func discoverFrontendFiles(frontendDir string) ([]string, error) {
	apiDir := filepath.Join(frontendDir, "src", "api")
	var files []string
	err := filepath.WalkDir(apiDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if (!strings.HasSuffix(name, ".ts") && !strings.HasSuffix(name, ".tsx")) ||
			strings.HasSuffix(name, ".test.ts") || strings.HasSuffix(name, ".test.tsx") ||
			strings.HasSuffix(name, ".spec.ts") || strings.HasSuffix(name, ".spec.tsx") ||
			strings.HasSuffix(name, ".d.ts") {
			return nil
		}
		rel, err := filepath.Rel(frontendDir, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("обход %s: %w", apiDir, err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("в %s не найдено runtime TypeScript-файлов", apiDir)
	}
	sort.Strings(files)
	return files, nil
}

func submatch(content string, m []int, group int) string {
	if m[2*group] < 0 {
		return ""
	}
	return content[m[2*group]:m[2*group+1]]
}

func normalizePath(raw string) string {
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		raw = raw[:i]
	}
	return normalizeTemplateExpressions(raw)
}

// normalizeTemplateExpressions заменяет динамический сегмент `${...}` на
// `{param}`, учитывая вложенные object literals в buildQuery({...}). Динамика,
// приклеенная к literal-сегменту без '/', является query builder/conditional и
// отбрасывается вместе с query-строкой.
func normalizeTemplateExpressions(raw string) string {
	var result strings.Builder
	for cursor := 0; cursor < len(raw); {
		start := strings.Index(raw[cursor:], "${")
		if start < 0 {
			result.WriteString(raw[cursor:])
			break
		}
		start += cursor
		result.WriteString(raw[cursor:start])
		if start > 0 && raw[start-1] != '/' {
			break
		}
		end := templateExpressionEnd(raw, start+2)
		if end < 0 {
			break
		}
		result.WriteString("{param}")
		cursor = end + 1
	}
	return strings.TrimRight(result.String(), " ")
}

func templateExpressionEnd(raw string, start int) int {
	depth := 1
	for index := start; index < len(raw); index++ {
		switch raw[index] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

// loadSpec возвращает множество "METHOD /path" из OpenAPI-документа.
func loadSpec(path string) (map[string]struct{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if len(doc.Paths) == 0 {
		return nil, fmt.Errorf("в документе нет paths")
	}
	methods := map[string]struct{}{}
	for specPath, operations := range doc.Paths {
		for op := range operations {
			switch op {
			case "get", "post", "put", "patch", "delete":
				methods[strings.ToUpper(op)+" "+specPath] = struct{}{}
			}
		}
	}
	return methods, nil
}

// matchSpec ищет операцию контракта, совпадающую по методу и сегментам пути;
// сегмент {param} фронтенда совпадает только с {…}-сегментом контракта.
func matchSpec(fullPath, method string, specMethods map[string]struct{}) (string, bool) {
	want := strings.Split(fullPath, "/")
	for key := range specMethods {
		specMethod, specPath, _ := strings.Cut(key, " ")
		if specMethod != method {
			continue
		}
		got := strings.Split(specPath, "/")
		if len(got) != len(want) {
			continue
		}
		ok := true
		for i := range want {
			specParam := strings.HasPrefix(got[i], "{")
			callParam := want[i] == "{param}"
			if specParam != callParam || (!specParam && got[i] != want[i]) {
				ok = false
				break
			}
		}
		if ok {
			return key, true
		}
	}
	return "", false
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "sync-contract: "+format+"\n", args...)
	os.Exit(1)
}
