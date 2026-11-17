package detect

import (
	"slices"
	"testing"
)

func names(ts []Tech) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.Name)
	}
	slices.Sort(out)
	return out
}

func TestInspectMonorepo(t *testing.T) {
	tree := []Entry{
		{"README.md", false}, {"LICENSE", false}, {".gitignore", false},
		{".github", true}, {".github/workflows", true}, {".github/workflows/ci.yml", false},
		{"backend", true}, {"backend/go.mod", false}, {"backend/Dockerfile", false},
		{"backend/internal", true}, {"backend/internal/auth", true}, {"backend/internal/auth/auth_test.go", false},
		{"frontend", true}, {"frontend/package.json", false}, {"frontend/tsconfig.json", false},
		{"frontend/node_modules/x/package.json", false}, // skipped
		{"docker-compose.yml", false},
	}
	f := Inspect(tree, "")
	if !f.HasReadme || !f.HasLicense || !f.HasGitignore || !f.HasCI || !f.HasTests {
		t.Fatalf("flags = %+v", f)
	}
	if want := []string{"backend/go.mod", "frontend/package.json"}; !slices.Equal(f.Manifests, want) {
		t.Fatalf("manifests = %v", f.Manifests)
	}
	if got := names(f.Present); !slices.Equal(got, []string{"Docker", "Docker Compose", "GitHub Actions", "TypeScript"}) {
		t.Fatalf("present = %v", got)
	}
}

func TestInspectBareRepo(t *testing.T) {
	f := Inspect([]Entry{{"main.py", false}, {"docs/README.md", false}}, "")
	if f.HasReadme || f.HasLicense || f.HasCI || f.HasTests {
		t.Fatalf("nested README must not count; flags = %+v", f)
	}
	if !Inspect(nil, "MIT").HasLicense {
		t.Fatal("GitHub-detected license should count")
	}
}

func TestTestDetection(t *testing.T) {
	for _, p := range []string{"src/app.test.tsx", "tests", "pkg/test_utils.py", "spec/models/user_spec.rb", "src/FooTest.java"} {
		isDir := p == "tests"
		if !Inspect([]Entry{{p, isDir}}, "").HasTests {
			t.Errorf("%s should count as tests", p)
		}
	}
	if Inspect([]Entry{{"src/testimony.go", false}, {"latest.py", false}}, "").HasTests {
		t.Error("false positive")
	}
}

func TestStack(t *testing.T) {
	contents := map[string]string{
		"backend/go.mod": "module x\n\ngo 1.27\n\nrequire (\n\tgithub.com/go-chi/chi/v5 v5.5.0\n\tgithub.com/jackc/pgx/v5 v5.13.0 // indirect\n)\nrequire github.com/stretchr/testify v1.9.0\n",
		"frontend/package.json": `{"dependencies":{"react":"^19","recharts":"^3"},"devDependencies":{"vite":"^7","@storybook/react":"^9","react-dom":"^19"}}`,
		"ml/requirements.txt":   "# deps\nnumpy==2.1\nscikit-learn>=1.5\n-r other.txt\nFastAPI[all]\n",
		"rs/Cargo.toml":         "[dependencies]\ntokio = { version = \"1\" }\nserde_json = \"1\"\n",
	}
	got := names(Stack(Files{Present: []Tech{{Name: "Docker"}}}, contents))
	want := []string{"Docker", "FastAPI", "NumPy", "React", "Recharts", "Storybook", "Tokio", "Vite", "chi", "pgx", "scikit-learn", "testify"}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("stack =\n%v\nwant\n%v", got, want)
	}
}

func TestStackWordBoundary(t *testing.T) {
	// "serde_json" must not match "serde"; "rails" must not match "railsish".
	got := names(Stack(Files{}, map[string]string{"Cargo.toml": `serde_json = "1"`, "Gemfile": `gem "railsish"`}))
	if len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestHasRealTestScript(t *testing.T) {
	if HasRealTestScript(`{"scripts":{"test":"echo \"Error: no test specified\" && exit 1"}}`) {
		t.Error("npm placeholder counted")
	}
	if !HasRealTestScript(`{"scripts":{"test":"vitest run"}}`) {
		t.Error("real script missed")
	}
}
