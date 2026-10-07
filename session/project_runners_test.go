package session

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// write creates a file in dir, failing the test rather than the function
// under test when the fixture itself cannot be made.
func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}

// commandsFor returns the suggested command lines for one tool.
func commandsFor(suggestions []RunnerSuggestion, tool string) []string {
	var out []string
	for _, s := range suggestions {
		if s.Tool == tool {
			out = append(out, s.Command)
		}
	}
	return out
}

func hasCommand(suggestions []RunnerSuggestion, command string) bool {
	for _, s := range suggestions {
		if s.Command == command {
			return true
		}
	}
	return false
}

// A directory that is not a build root must suggest nothing: most
// directories are not, and offering commands that cannot run there would
// make the list worthless everywhere else.
func TestSuggestRunnersIgnoresAnOrdinaryDirectory(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "README.md", "# nothing to build\n")
	if got := SuggestRunners(dir); len(got) != 0 {
		t.Errorf("SuggestRunners() = %v, want nothing", got)
	}
	if got := SuggestRunners(""); got != nil {
		t.Errorf("SuggestRunners(\"\") = %v, want nil", got)
	}
}

// The npm scripts offered are the ones really in package.json, so a project
// whose dev script is named something else still gets an offer that works.
func TestNpmSuggestionsReadTheRealScripts(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "package.json", `{"scripts":{"watch:all":"vite","test":"node --test"}}`)

	got := SuggestRunners(dir)
	if !hasCommand(got, "npm run watch:all") {
		t.Errorf("the project's own script was not offered: %v", commandsFor(got, "npm"))
	}
	// test and start are the two that take no "run".
	if !hasCommand(got, "npm test") {
		t.Errorf("npm test was not offered as a bare command: %v", commandsFor(got, "npm"))
	}
	// Nothing invented: this project has no dev script.
	if hasCommand(got, "npm run dev") {
		t.Error("a script that does not exist was offered")
	}
}

func TestNpmSuggestionsSurviveAnUnreadablePackageFile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "package.json", "{not json")
	if got := SuggestRunners(dir); len(got) != 0 {
		t.Errorf("SuggestRunners() = %v, want nothing from a broken file", got)
	}
}

// Make targets come from the file, and the lines that are not targets —
// variable assignments, .PHONY, pattern rules — must not become commands.
func TestMakeSuggestionsSkipNonTargets(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "Makefile", strings.Join([]string{
		"CC = gcc",
		".PHONY: build",
		"build:",
		"\t$(CC) -o app main.c",
		"%.o: %.c",
		"\t$(CC) -c $<",
		"deploy: build",
		"\t./deploy.sh",
	}, "\n"))

	got := commandsFor(SuggestRunners(dir), "make")
	for _, want := range []string{"make build", "make deploy"} {
		if !hasCommand(SuggestRunners(dir), want) {
			t.Errorf("%q was not offered: %v", want, got)
		}
	}
	for _, unwanted := range []string{"make CC", "make .PHONY", "make %.o"} {
		if hasCommand(SuggestRunners(dir), unwanted) {
			t.Errorf("%q is not a target but was offered: %v", unwanted, got)
		}
	}
}

// Gradle's server tasks are offered only when the build script mentions
// them: which one a project uses is written there, not guessable, and
// offering both to everyone would mean offering one that always fails.
func TestGradleSuggestionsFollowTheBuildScript(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "gradlew", "#!/bin/sh\n")
	if runtime.GOOS == "windows" {
		write(t, dir, "gradlew.bat", "@echo off\n")
	}
	write(t, dir, "build.gradle", "plugins { id 'org.springframework.boot' }\ntasks.named('bootRun')\n")

	got := SuggestRunners(dir)
	wrapper, ok := wrapperScript(dir)
	if !ok {
		t.Fatal("the wrapper was not detected")
	}
	if !hasCommand(got, wrapper+" bootRun") {
		t.Errorf("bootRun was not offered: %v", commandsFor(got, "gradle"))
	}
	if hasCommand(got, wrapper+" appRun") {
		t.Error("appRun was offered although the build script never mentions it")
	}
	// A long-running server belongs in a tab of its own.
	for _, s := range got {
		if s.Command == wrapper+" bootRun" && s.RunIn != RunInBackground {
			t.Errorf("bootRun runs in %q, want %q", s.RunIn, RunInBackground)
		}
	}
}

// A POM's plugins decide which server goal is offered, for the same reason.
func TestMavenSuggestionsFollowThePOM(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "pom.xml", "<project><build><plugins><plugin>"+
		"<artifactId>tomcat7-maven-plugin</artifactId>"+
		"</plugin></plugins></build></project>")

	got := SuggestRunners(dir)
	if !hasCommand(got, "mvn tomcat7:run") {
		t.Errorf("the declared Tomcat plugin was not offered: %v", commandsFor(got, "maven"))
	}
	if hasCommand(got, "mvn spring-boot:run") {
		t.Error("a plugin the POM does not declare was offered")
	}
	if !hasCommand(got, "mvn clean install") {
		t.Error("the lifecycle phases were not offered")
	}
}

// A standalone Tomcat is offered only when CATALINA_HOME points at a real
// one. A guessed path would be a command that fails for most people.
func TestTomcatSuggestionsNeedCatalinaHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CATALINA_HOME", "")
	if got := commandsFor(SuggestRunners(dir), "tomcat"); len(got) != 0 {
		t.Errorf("Tomcat was offered without CATALINA_HOME: %v", got)
	}

	// Set but pointing nowhere: still nothing, rather than a broken command.
	t.Setenv("CATALINA_HOME", filepath.Join(dir, "absent"))
	if got := commandsFor(SuggestRunners(dir), "tomcat"); len(got) != 0 {
		t.Errorf("Tomcat was offered for a missing install: %v", got)
	}

	home := filepath.Join(dir, "tomcat")
	if err := os.MkdirAll(filepath.Join(home, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	script := "catalina.sh"
	if runtime.GOOS == "windows" {
		script = "catalina.bat"
	}
	write(t, filepath.Join(home, "bin"), script, "#!/bin/sh\n")
	t.Setenv("CATALINA_HOME", home)

	got := commandsFor(SuggestRunners(dir), "tomcat")
	if len(got) != 1 || !strings.HasSuffix(got[0], " run") {
		// "run" stays in the foreground, so the tab shows the log; "startup"
		// daemonises and leaves a server behind a tab that looks finished.
		t.Errorf("Tomcat suggestion = %v, want one ending in \" run\"", got)
	}
}

func TestDockerSuggestionsAcceptEitherFileName(t *testing.T) {
	for _, name := range []string{"docker-compose.yml", "compose.yaml"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, name, "services: {}\n")
			if !hasCommand(SuggestRunners(dir), "docker compose up") {
				t.Errorf("compose was not detected from %s", name)
			}
		})
	}
}

// Every suggestion must name a target the library accepts, or adding one
// would be rejected on save.
func TestEverySuggestionHasAValidRunTarget(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "package.json", `{"scripts":{"dev":"vite","build":"vite build"}}`)
	write(t, dir, "Makefile", "all:\n\techo hi\n")
	write(t, dir, "pom.xml", "<project/>")
	write(t, dir, "compose.yml", "services: {}\n")

	got := SuggestRunners(dir)
	if len(got) == 0 {
		t.Fatal("nothing was suggested for a directory with four build files")
	}
	for _, s := range got {
		if !ValidRunIn(s.RunIn) {
			t.Errorf("%q suggests invalid target %q", s.Command, s.RunIn)
		}
		if strings.TrimSpace(s.Name) == "" || strings.TrimSpace(s.Command) == "" {
			t.Errorf("suggestion %+v has an empty name or command", s)
		}
	}
}
