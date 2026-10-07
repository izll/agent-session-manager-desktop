package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

// Suggested commands for the build tools a project directory turns out to
// use, offered for the saved-command library rather than run.
//
// Read from the files that are actually there, never from an IDE's project
// metadata. A `gradlew` in the directory is a fact; an IntelliJ run
// configuration is a description of what that IDE does with it, kept partly
// in workspace.xml, which is not in version control and so differs from one
// machine to the next. Worse, the interesting ones — a Tomcat server, an
// application server deployment — are not commands at all but processes the
// IDE orchestrates, and a command line claiming to reproduce one would work
// until the day it quietly did something else.
//
// So: a short list per tool, derived from files on disk, which the user adds
// to their library and edits from there. Nothing here runs anything.

// RunnerSuggestion is one command a project's files suggest.
type RunnerSuggestion struct {
	// Tool is the build tool this came from ("gradle", "maven", "npm",
	// "make", "docker", "tomcat"), for grouping in the UI.
	Tool string `json:"tool"`
	// Name is the suggested command's label in the library.
	Name string `json:"name"`
	// Command is the shell text, relative to the tab's directory.
	Command string `json:"command"`
	// RunIn is the target that suits this command: a server or a watching
	// build wants a tab of its own, a one-shot test run does not.
	RunIn string `json:"runIn"`
}

// Guard rails for reading project files: these are read on a UI request, so
// a pathological file must not become a pause.
const (
	runnerFileMaxBytes = 512 * 1024
	// npm scripts and make targets are offered in full when there are few;
	// past this many, the well-known ones are offered and the rest left to
	// the user, since a list nobody can scan is not a suggestion.
	runnerMaxScripts = 24
)

// wrapperScript is the Gradle wrapper's name on this platform. A repository
// carries both, and offering `./gradlew` to a Windows user would not run.
func wrapperScript(dir string) (string, bool) {
	if runtime.GOOS == "windows" {
		if fileExists(filepath.Join(dir, "gradlew.bat")) {
			return "gradlew.bat", true
		}
		// A repo with only the Unix wrapper still builds under a shell that
		// has one, so fall through rather than offering nothing.
	}
	if fileExists(filepath.Join(dir, "gradlew")) {
		return "./gradlew", true
	}
	if runtime.GOOS == "windows" && fileExists(filepath.Join(dir, "gradlew.bat")) {
		return "gradlew.bat", true
	}
	return "", false
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// SuggestRunners returns the commands dir's files suggest, grouped by tool
// and in a stable order.
//
// An empty result means nothing recognisable was found, which is not an
// error: most directories are not build roots.
func SuggestRunners(dir string) []RunnerSuggestion {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	var out []RunnerSuggestion
	out = append(out, gradleSuggestions(dir)...)
	out = append(out, mavenSuggestions(dir)...)
	out = append(out, npmSuggestions(dir)...)
	out = append(out, makeSuggestions(dir)...)
	out = append(out, dockerSuggestions(dir)...)
	out = append(out, tomcatSuggestions(dir)...)
	return out
}

// gradleSuggestions offers the wrapper's common tasks.
//
// bootRun and appRun are both offered when the build mentions them: Spring
// Boot's embedded server and the Gretty/Tomcat plugins are how a Gradle
// project is usually served, and which one a project uses is written in its
// build script rather than guessable.
func gradleSuggestions(dir string) []RunnerSuggestion {
	wrapper, ok := wrapperScript(dir)
	if !ok {
		return nil
	}
	out := []RunnerSuggestion{
		{Tool: "gradle", Name: "Gradle build", Command: wrapper + " build", RunIn: RunInNewTab},
		{Tool: "gradle", Name: "Gradle test", Command: wrapper + " test", RunIn: RunInNewTab},
	}
	script := readBuildScript(dir, "build.gradle", "build.gradle.kts")
	for _, task := range []struct{ name, task string }{
		{"Spring Boot run", "bootRun"},
		{"Gretty app run", "appRun"},
	} {
		if strings.Contains(script, task.task) {
			out = append(out, RunnerSuggestion{
				Tool: "gradle", Name: task.name,
				Command: wrapper + " " + task.task,
				// A server keeps running: it belongs in a tab of its own,
				// left in the background so starting it does not take over
				// the tab it was started from.
				RunIn: RunInBackground,
			})
		}
	}
	return out
}

// mavenSuggestions offers the lifecycle phases and whichever server plugin
// the POM actually declares.
func mavenSuggestions(dir string) []RunnerSuggestion {
	if !fileExists(filepath.Join(dir, "pom.xml")) {
		return nil
	}
	out := []RunnerSuggestion{
		{Tool: "maven", Name: "Maven install", Command: "mvn clean install", RunIn: RunInNewTab},
		{Tool: "maven", Name: "Maven test", Command: "mvn test", RunIn: RunInNewTab},
	}
	pom := readBuildScript(dir, "pom.xml")
	// Plugin prefixes rather than artifact ids: this is what gets typed, and
	// a POM naming the artifact is the signal that the prefix will resolve.
	for _, plugin := range []struct{ name, artifact, goal string }{
		{"Spring Boot run", "spring-boot-maven-plugin", "mvn spring-boot:run"},
		{"Tomcat 7 run", "tomcat7-maven-plugin", "mvn tomcat7:run"},
		{"Jetty run", "jetty-maven-plugin", "mvn jetty:run"},
	} {
		if strings.Contains(pom, plugin.artifact) {
			out = append(out, RunnerSuggestion{
				Tool: "maven", Name: plugin.name, Command: plugin.goal, RunIn: RunInBackground,
			})
		}
	}
	return out
}

// npmSuggestions reads the scripts that are really in package.json rather
// than guessing the usual names, so a project whose dev script is called
// something else still gets an offer that works.
func npmSuggestions(dir string) []RunnerSuggestion {
	path := filepath.Join(dir, "package.json")
	if !fileExists(path) {
		return nil
	}
	data, err := readFileAtMost(path, runnerFileMaxBytes)
	if err != nil {
		return nil
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil || len(pkg.Scripts) == 0 {
		return nil
	}
	names := make([]string, 0, len(pkg.Scripts))
	for name := range pkg.Scripts {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > runnerMaxScripts {
		names = wellKnownFirst(names)
	}

	out := make([]RunnerSuggestion, 0, len(names))
	for _, name := range names {
		command := "npm run " + name
		// `npm test` and `npm start` are the two that need no "run".
		if name == "test" || name == "start" {
			command = "npm " + name
		}
		out = append(out, RunnerSuggestion{
			Tool: "npm", Name: "npm: " + name, Command: command, RunIn: npmRunTarget(name),
		})
	}
	return out
}

// npmRunTarget guesses from the script's name whether it keeps running.
// Wrong either way it is only a default the user can change, so this errs
// towards a new tab, which is visible, over the background, which is not.
func npmRunTarget(name string) string {
	switch name {
	case "dev", "start", "serve", "watch", "preview":
		return RunInBackground
	}
	return RunInNewTab
}

// wellKnownFirst keeps the scripts people actually reach for when a
// package.json has too many to offer them all.
func wellKnownFirst(names []string) []string {
	known := map[string]bool{
		"dev": true, "start": true, "build": true, "test": true,
		"lint": true, "serve": true, "watch": true, "check": true,
		"preview": true, "format": true,
	}
	out := make([]string, 0, len(known))
	for _, name := range names {
		if known[name] {
			out = append(out, name)
		}
	}
	return out
}

// makeTargetRe matches a target definition at the start of a line, skipping
// pattern rules (%), the special dot-targets (.PHONY) and variable
// assignments, none of which are things to run.
var makeTargetRe = regexp.MustCompile(`(?m)^([a-zA-Z0-9][a-zA-Z0-9_.-]*)\s*:(?:[^=]|$)`)

// makeSuggestions reads the real targets out of a Makefile.
func makeSuggestions(dir string) []RunnerSuggestion {
	name := "Makefile"
	if !fileExists(filepath.Join(dir, name)) {
		name = "makefile"
		if !fileExists(filepath.Join(dir, name)) {
			return nil
		}
	}
	data, err := readFileAtMost(filepath.Join(dir, name), runnerFileMaxBytes)
	if err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var out []RunnerSuggestion
	for _, match := range makeTargetRe.FindAllStringSubmatch(string(data), -1) {
		target := match[1]
		if seen[target] || strings.Contains(target, "%") {
			continue
		}
		seen[target] = true
		out = append(out, RunnerSuggestion{
			Tool: "make", Name: "make " + target, Command: "make " + target, RunIn: RunInNewTab,
		})
		if len(out) >= runnerMaxScripts {
			break
		}
	}
	return out
}

// dockerSuggestions offers compose when a compose file is present. `docker
// compose` (the plugin) rather than `docker-compose` (the retired standalone
// script), with no -d: the point of a tab of its own is to read the logs.
func dockerSuggestions(dir string) []RunnerSuggestion {
	for _, name := range []string{
		"docker-compose.yml", "docker-compose.yaml",
		"compose.yml", "compose.yaml",
	} {
		if !fileExists(filepath.Join(dir, name)) {
			continue
		}
		return []RunnerSuggestion{
			{Tool: "docker", Name: "Compose up", Command: "docker compose up", RunIn: RunInBackground},
			{Tool: "docker", Name: "Compose down", Command: "docker compose down", RunIn: RunInNewTab},
		}
	}
	return nil
}

// tomcatSuggestions offers a standalone Tomcat only when the environment
// says where one is.
//
// This is the separately installed case, not the embedded or plugin ones the
// Gradle and Maven detectors cover. There is nothing in the project
// directory that reveals it, so CATALINA_HOME is the only honest signal; a
// guessed /opt/tomcat would be a command that fails for most people.
func tomcatSuggestions(dir string) []RunnerSuggestion {
	home := strings.TrimSpace(os.Getenv("CATALINA_HOME"))
	if home == "" {
		return nil
	}
	script := filepath.Join(home, "bin", "catalina.sh")
	if runtime.GOOS == "windows" {
		script = filepath.Join(home, "bin", "catalina.bat")
	}
	if !fileExists(script) {
		return nil
	}
	// `run` rather than `startup`: it stays in the foreground, so the tab
	// shows the log and closing it stops the server. `startup` daemonises,
	// leaving a server running behind a tab that looks finished.
	return []RunnerSuggestion{
		{Tool: "tomcat", Name: "Tomcat run", Command: script + " run", RunIn: RunInBackground},
	}
}

// readBuildScript returns the first of names that can be read, as a string,
// or "" when none can. Used only to look for a task name, so a missing or
// oversized file simply means "not mentioned".
func readBuildScript(dir string, names ...string) string {
	for _, name := range names {
		if data, err := readFileAtMost(filepath.Join(dir, name), runnerFileMaxBytes); err == nil {
			return string(data)
		}
	}
	return ""
}
