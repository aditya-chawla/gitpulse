// Package detect inspects a repository's file tree and manifest files to find
// health signals (README, CI, tests…) and the frameworks/tools it uses. Pure:
// callers supply the tree and file contents.
package detect

import (
	"encoding/json"
	"path"
	"regexp"
	"slices"
	"strings"
)

type Entry struct {
	Path  string
	IsDir bool
}

type Files struct {
	HasReadme    bool
	HasLicense   bool
	HasGitignore bool
	HasCI        bool
	HasTests     bool
	// Manifests are paths whose contents should be fetched for tech detection.
	Manifests []string
	// Present holds marker files/dirs that identify tools by presence alone.
	Present []Tech
}

type Tech struct {
	Name     string `json:"name"`
	Category string `json:"category"` // framework | library | database | testing | build | infra | tooling
	Source   string `json:"source"`
}

// Directories whose contents are never the repo's own code.
var skipDirs = map[string]bool{"node_modules": true, "vendor": true, "third_party": true, ".venv": true, "venv": true, "dist": true, "build": true}

var parseable = map[string]bool{
	"go.mod": true, "package.json": true, "requirements.txt": true, "pyproject.toml": true,
	"Pipfile": true, "Cargo.toml": true, "Gemfile": true, "composer.json": true,
	"pom.xml": true, "build.gradle": true, "build.gradle.kts": true, "pubspec.yaml": true,
}

var testDirs = map[string]bool{"test": true, "tests": true, "__tests__": true, "spec": true, "e2e": true, "testing": true}

// Case-sensitive on purpose: "FooTest.java" is a test, "Latest.java" is not.
var testFile = regexp.MustCompile(`(_test\.go|\.(test|spec)\.[cm]?[jt]sx?|^test_.*\.py|_test\.py|Tests?\.(java|kt|cs)|_spec\.rb)$`)

func skipped(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if skipDirs[seg] {
			return true
		}
	}
	return false
}

// Inspect classifies a flattened tree. licenseSPDX is GitHub's detected
// license (empty if none), which also counts as having a license.
func Inspect(tree []Entry, licenseSPDX string) Files {
	f := Files{HasLicense: licenseSPDX != ""}
	seen := map[string]bool{}
	present := func(name, cat, src string) {
		if !seen[name] {
			seen[name] = true
			f.Present = append(f.Present, Tech{Name: name, Category: cat, Source: src})
		}
	}
	for _, e := range tree {
		if skipped(e.Path) {
			continue
		}
		base := path.Base(e.Path)
		lower := strings.ToLower(base)
		root := !strings.Contains(e.Path, "/")

		if e.IsDir {
			if testDirs[lower] {
				f.HasTests = true
			}
			if e.Path == ".circleci" {
				f.HasCI = true
			}
			continue
		}
		if root {
			switch {
			case strings.HasPrefix(lower, "readme"):
				f.HasReadme = true
			case strings.HasPrefix(lower, "license"), strings.HasPrefix(lower, "licence"), strings.HasPrefix(lower, "copying"):
				f.HasLicense = true
			case base == ".gitignore":
				f.HasGitignore = true
			case base == ".gitlab-ci.yml", base == ".travis.yml", base == "azure-pipelines.yml", base == "Jenkinsfile":
				f.HasCI = true
			}
		}
		if strings.HasPrefix(e.Path, ".github/workflows/") && (strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".yaml")) {
			f.HasCI = true
			present("GitHub Actions", "infra", e.Path)
		}
		if testFile.MatchString(base) {
			f.HasTests = true
		}
		if parseable[base] {
			f.Manifests = append(f.Manifests, e.Path)
		}
		switch {
		case base == "Dockerfile" || strings.HasSuffix(base, ".Dockerfile"):
			present("Docker", "infra", e.Path)
		case lower == "docker-compose.yml" || lower == "docker-compose.yaml" || lower == "compose.yaml" || lower == "compose.yml":
			present("Docker Compose", "infra", e.Path)
		case strings.HasSuffix(lower, ".tf"):
			present("Terraform", "infra", e.Path)
		case base == "Makefile":
			present("Make", "build", e.Path)
		case base == "render.yaml":
			present("Render", "infra", e.Path)
		case base == "vercel.json":
			present("Vercel", "infra", e.Path)
		case base == "netlify.toml":
			present("Netlify", "infra", e.Path)
		case base == "fly.toml":
			present("Fly.io", "infra", e.Path)
		case base == "tsconfig.json":
			present("TypeScript", "tooling", e.Path)
		}
	}
	slices.Sort(f.Manifests)
	return f
}

type rule struct {
	match    string // dependency name, or prefix when it ends with "/" or "*"
	name     string
	category string
}

var goRules = []rule{
	{"github.com/go-chi/chi", "chi", "framework"}, {"github.com/gin-gonic/gin", "Gin", "framework"},
	{"github.com/labstack/echo", "Echo", "framework"}, {"github.com/gofiber/fiber", "Fiber", "framework"},
	{"github.com/gorilla/mux", "Gorilla Mux", "framework"}, {"github.com/jackc/pgx", "pgx", "database"},
	{"gorm.io/gorm", "GORM", "database"}, {"github.com/spf13/cobra", "Cobra", "library"},
	{"github.com/stretchr/testify", "testify", "testing"}, {"google.golang.org/grpc", "gRPC", "library"},
	{"github.com/redis/go-redis", "Redis", "database"}, {"go.mongodb.org/mongo-driver", "MongoDB", "database"},
	{"github.com/charmbracelet/bubbletea", "Bubble Tea", "library"}, {"github.com/aws/aws-sdk-go", "AWS SDK", "library"},
	{"github.com/golang-migrate/migrate", "golang-migrate", "database"}, {"github.com/mattn/go-sqlite3", "SQLite", "database"},
}

var npmRules = []rule{
	{"react", "React", "framework"}, {"next", "Next.js", "framework"}, {"vue", "Vue", "framework"},
	{"nuxt", "Nuxt", "framework"}, {"svelte", "Svelte", "framework"}, {"@sveltejs/kit", "SvelteKit", "framework"},
	{"@angular/core", "Angular", "framework"}, {"solid-js", "Solid", "framework"}, {"astro", "Astro", "framework"},
	{"express", "Express", "framework"}, {"fastify", "Fastify", "framework"}, {"koa", "Koa", "framework"},
	{"@nestjs/core", "NestJS", "framework"}, {"electron", "Electron", "framework"}, {"react-native", "React Native", "framework"},
	{"vite", "Vite", "build"}, {"webpack", "webpack", "build"}, {"tailwindcss", "Tailwind CSS", "library"},
	{"typescript", "TypeScript", "tooling"}, {"eslint", "ESLint", "tooling"}, {"prettier", "Prettier", "tooling"},
	{"jest", "Jest", "testing"}, {"vitest", "Vitest", "testing"}, {"mocha", "Mocha", "testing"},
	{"cypress", "Cypress", "testing"}, {"@playwright/test", "Playwright", "testing"},
	{"recharts", "Recharts", "library"}, {"chart.js", "Chart.js", "library"}, {"d3", "D3", "library"},
	{"three", "three.js", "library"}, {"@reduxjs/toolkit", "Redux", "library"}, {"redux", "Redux", "library"},
	{"zustand", "Zustand", "library"}, {"@tanstack/react-query", "TanStack Query", "library"},
	{"prisma", "Prisma", "database"}, {"@prisma/client", "Prisma", "database"}, {"mongoose", "MongoDB", "database"},
	{"pg", "PostgreSQL", "database"}, {"socket.io", "Socket.IO", "library"}, {"graphql", "GraphQL", "library"},
	{"@storybook/*", "Storybook", "tooling"},
}

var pyRules = []rule{
	{"django", "Django", "framework"}, {"flask", "Flask", "framework"}, {"fastapi", "FastAPI", "framework"},
	{"streamlit", "Streamlit", "framework"}, {"pandas", "pandas", "library"}, {"numpy", "NumPy", "library"},
	{"scipy", "SciPy", "library"}, {"scikit-learn", "scikit-learn", "library"}, {"torch", "PyTorch", "library"},
	{"tensorflow", "TensorFlow", "library"}, {"keras", "Keras", "library"}, {"transformers", "Hugging Face Transformers", "library"},
	{"langchain", "LangChain", "library"}, {"matplotlib", "Matplotlib", "library"}, {"sqlalchemy", "SQLAlchemy", "database"},
	{"pydantic", "Pydantic", "library"}, {"celery", "Celery", "library"}, {"pytest", "pytest", "testing"},
	{"requests", "Requests", "library"}, {"beautifulsoup4", "Beautiful Soup", "library"}, {"selenium", "Selenium", "testing"},
	{"opencv-python", "OpenCV", "library"}, {"jupyter", "Jupyter", "tooling"},
}

// Text-searched manifests (TOML, Gemfile, Maven, Gradle, YAML): a word-boundary
// match on the dependency name is accurate enough for these formats.
var textRules = map[string][]rule{
	"Cargo.toml": {{"tokio", "Tokio", "library"}, {"actix-web", "Actix Web", "framework"}, {"axum", "Axum", "framework"},
		{"rocket", "Rocket", "framework"}, {"serde", "Serde", "library"}, {"clap", "clap", "library"},
		{"diesel", "Diesel", "database"}, {"sqlx", "SQLx", "database"}, {"bevy", "Bevy", "framework"}, {"tauri", "Tauri", "framework"}},
	"Gemfile": {{"rails", "Ruby on Rails", "framework"}, {"sinatra", "Sinatra", "framework"},
		{"rspec", "RSpec", "testing"}, {"jekyll", "Jekyll", "framework"}},
	"composer.json": {{"laravel/framework", "Laravel", "framework"}, {"symfony/framework-bundle", "Symfony", "framework"},
		{"phpunit/phpunit", "PHPUnit", "testing"}},
	"pom.xml": {{"spring-boot", "Spring Boot", "framework"}, {"junit", "JUnit", "testing"}, {"hibernate", "Hibernate", "database"}},
	"build.gradle": {{"spring-boot", "Spring Boot", "framework"}, {"junit", "JUnit", "testing"},
		{"com.android.application", "Android", "framework"}},
	"pubspec.yaml": {{"flutter", "Flutter", "framework"}},
}

func init() {
	textRules["build.gradle.kts"] = textRules["build.gradle"]
	textRules["pyproject.toml"] = pyRules
	textRules["Pipfile"] = pyRules
}

func (r rule) matches(dep string) bool {
	if p, ok := strings.CutSuffix(r.match, "*"); ok {
		return strings.HasPrefix(dep, p)
	}
	// Go module paths may carry a /vN suffix.
	return dep == r.match || strings.HasPrefix(dep, r.match+"/")
}

// Stack derives tech from fetched manifest contents (path -> text) plus the
// presence markers from Inspect. Results are de-duplicated by name.
func Stack(files Files, contents map[string]string) []Tech {
	out := slices.Clone(files.Present)
	seen := map[string]bool{}
	for _, t := range out {
		seen[t.Name] = true
	}
	add := func(rules []rule, deps []string, src string) {
		for _, d := range deps {
			for _, r := range rules {
				if !seen[r.name] && r.matches(d) {
					seen[r.name] = true
					out = append(out, Tech{Name: r.name, Category: r.category, Source: src})
				}
			}
		}
	}
	paths := make([]string, 0, len(contents))
	for p := range contents {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	for _, p := range paths {
		text := contents[p]
		switch base := path.Base(p); base {
		case "go.mod":
			add(goRules, goModDeps(text), p)
		case "package.json":
			add(npmRules, packageJSONDeps(text), p)
		case "requirements.txt":
			add(pyRules, requirementsDeps(text), p)
		default:
			if rules, ok := textRules[base]; ok {
				for _, r := range rules {
					if !seen[r.name] && containsWord(text, r.match) {
						seen[r.name] = true
						out = append(out, Tech{Name: r.name, Category: r.category, Source: p})
					}
				}
			}
		}
	}
	return out
}

func goModDeps(text string) []string {
	var deps []string
	inBlock := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, "//"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		switch {
		case line == "require (":
			inBlock = true
		case inBlock && line == ")":
			inBlock = false
		case inBlock && line != "":
			deps = append(deps, strings.Fields(line)[0])
		case strings.HasPrefix(line, "require "):
			if f := strings.Fields(line); len(f) >= 2 {
				deps = append(deps, f[1])
			}
		}
	}
	return deps
}

func packageJSONDeps(text string) []string {
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if json.Unmarshal([]byte(text), &pkg) != nil {
		return nil
	}
	var deps []string
	for d := range pkg.Dependencies {
		deps = append(deps, d)
	}
	for d := range pkg.DevDependencies {
		deps = append(deps, d)
	}
	slices.Sort(deps)
	return deps
}

// HasRealTestScript reports whether package.json defines a test script other
// than npm's placeholder.
func HasRealTestScript(packageJSON string) bool {
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal([]byte(packageJSON), &pkg) != nil {
		return false
	}
	s := strings.TrimSpace(pkg.Scripts["test"])
	return s != "" && !strings.Contains(s, "no test specified")
}

var reqName = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._-]*)`)

func requirementsDeps(text string) []string {
	var deps []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
			continue
		}
		if m := reqName.FindString(line); m != "" {
			deps = append(deps, strings.ToLower(m))
		}
	}
	return deps
}

func containsWord(text, word string) bool {
	re := regexp.MustCompile(`(^|[^A-Za-z0-9_-])` + regexp.QuoteMeta(word) + `($|[^A-Za-z0-9_-])`)
	return re.MatchString(text)
}
